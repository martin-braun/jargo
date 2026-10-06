//bin/sh -c 'exec /usr/bin/env go run "$(dirname "$0")" "$@"' "$0" "$@"; exit $?
// Command custom serves a browser voice bot using three independent API endpoints.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gojargo/jargo/audio/opus"
	"github.com/gojargo/jargo/audio/turn"
	"github.com/gojargo/jargo/audio/vad"
	"github.com/gojargo/jargo/frames"
	"github.com/gojargo/jargo/pipeline"
	"github.com/gojargo/jargo/processor/aggregators"
	"github.com/gojargo/jargo/processor/turns"
	"github.com/gojargo/jargo/processor/vadproc"
	"github.com/gojargo/jargo/provider/openai/chat"
	"github.com/gojargo/jargo/provider/openai/responses"
	"github.com/gojargo/jargo/transport"
	"github.com/gojargo/jargo/transport/rtc"
	"github.com/gojargo/jargo/utils/events"
	"github.com/pion/webrtc/v4"
)

//go:embed static
var staticFiles embed.FS

const systemPrompt = "You are a friendly voice assistant. Keep your replies short, " +
	"warm and conversational — one or two sentences."

//nolint:gochecknoglobals // sentinel errors
var (
	errVADUnavailable = errors.New("cannot load Silero VAD: ONNX Runtime is required; " +
		"check JARGO_ONNXRUNTIME_LIB in your shell")
	errTurnUnavailable = errors.New("cannot load Smart Turn: check ONNX Runtime compatibility " +
		"and JARGO_ONNXRUNTIME_LIB in your shell")
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	// Providers may include server error bodies in their diagnostics.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString || a.Value.Kind() == slog.KindAny {
				text := a.Value.String()
				for _, key := range []string{cfg.stt.APIKey, cfg.llm.APIKey, cfg.tts.APIKey} {
					text = strings.ReplaceAll(text, key, "[redacted]")
				}
				a.Value = slog.StringValue(text)
			}
			return a
		},
	})))
	// Segmented STT cannot fall back to server-side endpointing.
	vd, tr, err := newAnalyzers()
	if err != nil {
		return err
	}
	_ = vd.Close()
	_ = tr.Close()
	static, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /audio-config", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			SampleRate int `json:"sample_rate"`
			Channels   int `json:"channels"`
		}{cfg.stt.SampleRate, cfg.sttChannels})
	})
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("POST /offer", func(w http.ResponseWriter, r *http.Request) { handleOffer(w, r, cfg) })
	slog.Info("custom voice bot listening", "url", "http://localhost:8080")
	return http.ListenAndServe("127.0.0.1:8080", mux)
}

func newAnalyzers() (*vad.Silero, *turn.SmartTurnV3, error) {
	vd, err := vad.NewSilero()
	if err != nil {
		return nil, nil, errVADUnavailable
	}
	tr, err := turn.NewSmartTurnV3()
	if err != nil {
		_ = vd.Close()
		return nil, nil, errTurnUnavailable
	}
	return vd, tr, nil
}

func handleOffer(w http.ResponseWriter, r *http.Request, cfg config) {
	var offer webrtc.SessionDescription
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil || offer.Type != webrtc.SDPTypeOffer {
		http.Error(w, "expected a WebRTC SDP offer", http.StatusBadRequest)
		return
	}
	vd, tr, err := newAnalyzers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = vd.Close()
			_ = tr.Close()
		}
	}()
	conn, err := rtc.NewConnection(rtc.WithICEServers())
	if err != nil {
		http.Error(w, "cannot create WebRTC connection", http.StatusInternalServerError)
		return
	}
	answer, err := conn.Answer(offer)
	if err != nil {
		_ = conn.Close()
		http.Error(w, "cannot answer WebRTC offer", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(answer); err != nil {
		_ = conn.Close()
		return
	}
	transferred = true
	go runBot(conn, cfg, vd, tr)
}

func runBot(conn *rtc.Connection, cfg config, vd *vad.Silero, tr *turn.SmartTurnV3) {
	defer func() {
		_ = conn.Close()
		_ = vd.Close()
		_ = tr.Close()
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-conn.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	// Abandoned signaling must not retain an idle pipeline and model sessions.
	connectCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	err := conn.WaitConnected(connectCtx)
	stop()
	if err != nil {
		return
	}
	params := transport.DefaultParams()
	params.AudioInSampleRate = cfg.stt.SampleRate
	params.AudioInChannels = cfg.sttChannels
	params.AudioOutSampleRate = opus.SampleRate
	t := rtc.NewTransport(conn, params)
	agg := aggregators.New(frames.NewLLMContext(systemPrompt), aggregators.WithTurns(turns.Config{
		Strategies: turns.UserTurnStrategies{
			Start: turns.DefaultStartStrategies(),
			Stop:  []turns.StopStrategy{turns.NewTurnAnalyzerStop(turns.TurnAnalyzerConfig{Analyzer: tr})},
		},
	}))
	pipe := pipeline.New(
		t.Input(), vadproc.New(vadproc.Config{VAD: vd}), chat.NewSTT(cfg.stt),
		agg.User(), responses.NewHTTPLLM(cfg.llm), chat.NewTTS(cfg.tts), t.Output(), agg.Assistant(),
	)
	enableRTVI := false
	task := pipeline.NewWorker(pipe, pipeline.WorkerConfig{
		EnableRTVI: &enableRTVI,
		Params: pipeline.Params{
			AudioInSampleRate: cfg.stt.SampleRate, AudioOutSampleRate: opus.SampleRate,
		},
	})
	events.On(&task.Registry, pipeline.EventPipelineError, func(_ context.Context, _ *frames.ErrorFrame) {
		// Never forward provider error bodies or credentials to the browser.
		_ = conn.SendMessage([]byte(`{"type":"error",` +
			`"message":"Voice pipeline error; check server logs and endpoint contracts."}`))
	})
	if err = task.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("voice pipeline stopped; check service configuration and runtime")
	}
}
