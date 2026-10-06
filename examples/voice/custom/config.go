package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gojargo/jargo/provider/openai/chat"
	"github.com/gojargo/jargo/provider/openai/responses"
	"github.com/spf13/viper"
)

const envFile = "examples/voice/custom/.env"

//nolint:gochecknoglobals // sentinel errors
var (
	errReadConfig      = errors.New("cannot read examples/voice/custom/.env: check permissions and dotenv syntax")
	errRequiredConfig  = errors.New("required setting missing")
	errInvalidConfig   = errors.New("configuration invalid")
	errAbsoluteBaseURL = errors.New("use an absolute http(s) URL ending in /v1, without an operation suffix")
	errBaseURL         = errors.New("use a base ending in /v1; no credentials, query, fragment, or operation suffix")
)

type config struct {
	stt         chat.STTConfig
	llm         responses.Config
	tts         chat.TTSConfig
	sttChannels int
}

func loadConfig() (config, error) {
	v := viper.New()
	v.SetConfigFile(envFile)
	v.SetConfigType("env")
	v.SetDefault("STT_SAMPLE_RATE", "16000")
	v.SetDefault("STT_CHANNELS", "1")
	v.SetDefault("TTS_SAMPLE_RATE", "24000")
	v.SetDefault("TTS_CHANNELS", "1")
	v.AutomaticEnv()
	v.AllowEmptyEnv(true)
	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Parser errors can contain file contents, including credentials.
		return config{}, errReadConfig
	}
	for _, key := range []string{
		"LLM_BASE_URL", "LLM_MODEL", "STT_BASE_URL", "STT_MODEL", "TTS_BASE_URL", "TTS_MODEL", "TTS_VOICE",
	} {
		if strings.TrimSpace(v.GetString(key)) == "" {
			return config{}, fmt.Errorf("%s: %w; set it in the environment or %s (see .env.example)",
				key, errRequiredConfig, envFile)
		}
	}
	bases := make(map[string]string)
	for _, service := range []string{"LLM", "STT", "TTS"} {
		key := service + "_BASE_URL"
		base, err := normalizeBaseURL(v.GetString(key))
		if err != nil {
			return config{}, fmt.Errorf("%s: %w", key, err)
		}
		bases[service] = base
	}
	rate, channels, err := audioFormat(v)
	if err != nil {
		return config{}, err
	}
	ttsRate, ttsChannels, err := ttsAudioFormat(v)
	if err != nil {
		return config{}, err
	}
	cfg := config{
		sttChannels: channels,
		stt: chat.STTConfig{
			BaseURL: bases["STT"], Model: strings.TrimSpace(v.GetString("STT_MODEL")),
			APIKey: serviceKey(v.GetString("STT_API_KEY")), SampleRate: rate,
		},
		llm: responses.Config{
			BaseURL: bases["LLM"], Model: strings.TrimSpace(v.GetString("LLM_MODEL")),
			APIKey: serviceKey(v.GetString("LLM_API_KEY")),
		},
		tts: chat.TTSConfig{
			BaseURL: bases["TTS"], Model: strings.TrimSpace(v.GetString("TTS_MODEL")),
			APIKey: serviceKey(v.GetString("TTS_API_KEY")), Voice: strings.TrimSpace(v.GetString("TTS_VOICE")),
			SampleRate: ttsRate, Channels: ttsChannels,
		},
	}
	for _, item := range []struct {
		name string
		cfg  interface{ Validate() error }
	}{{"STT", cfg.stt}, {"LLM", cfg.llm}, {"TTS", cfg.tts}} {
		if err := item.cfg.Validate(); err != nil {
			return config{}, fmt.Errorf("%s: %w; check its settings in %s", item.name, errInvalidConfig, envFile)
		}
	}
	return cfg, nil
}

func audioFormat(v *viper.Viper) (int, int, error) {
	rate, err := strconv.Atoi(strings.TrimSpace(v.GetString("STT_SAMPLE_RATE")))
	if err != nil {
		return 0, 0, fmt.Errorf("STT_SAMPLE_RATE: %w; use an integer sample rate", errInvalidConfig)
	}
	switch rate {
	case 8000, 12000, 16000, 24000, 48000:
	default:
		return 0, 0, fmt.Errorf("STT_SAMPLE_RATE: %w; use 8000, 12000, 16000, 24000 or 48000", errInvalidConfig)
	}
	channels, err := strconv.Atoi(strings.TrimSpace(v.GetString("STT_CHANNELS")))
	if err != nil || channels != 1 {
		return 0, 0, fmt.Errorf("STT_CHANNELS: %w; this STT and turn-taking pipeline requires 1 (mono)", errInvalidConfig)
	}
	return rate, channels, nil
}

func ttsAudioFormat(v *viper.Viper) (int, int, error) {
	rate, err := strconv.Atoi(strings.TrimSpace(v.GetString("TTS_SAMPLE_RATE")))
	if err != nil || rate <= 0 {
		return 0, 0, fmt.Errorf("TTS_SAMPLE_RATE: %w; use a positive integer source PCM rate", errInvalidConfig)
	}
	channels, err := strconv.Atoi(strings.TrimSpace(v.GetString("TTS_CHANNELS")))
	if err != nil || channels != 1 {
		return 0, 0, fmt.Errorf("TTS_CHANNELS: %w; use 1 (mono); output does not downmix stereo", errInvalidConfig)
	}
	return rate, channels, nil
}

func normalizeBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errAbsoluteBaseURL
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		!strings.HasSuffix(u.Path, "/v1") || strings.Contains(u.Path, "//") || u.RawPath != "" {
		return "", errBaseURL
	}
	return base, nil
}

func serviceKey(key string) string {
	if strings.TrimSpace(key) == "" {
		// These clients validate nonempty keys and always send a Bearer header.
		return "local-no-auth"
	}
	return key
}
