---
title: Quickstart
weight: 2
---

# Quickstart

The example bots live in [`examples/`](../../examples):

- **echo**: speak into the browser, hear yourself back. No API keys.
- **voicebot**: the full voice agent. STT → LLM → TTS with turn-taking and
  barge-in, plus long-term memory and tracing.
- **voice/**: one headless backend per provider, each wiring its STT/LLM/TTS
  explicitly in Go and exposing only the WebRTC `/offer` endpoint
  (`go run ./examples/voice/<provider>`); drive it from a browser client.
- **custom**: local STT/LLM/TTS with an embedded UI
  (`go run ./examples/voice/custom`).

Run them with Docker (no host setup) or with a local Go toolchain.

## Run with Docker

jargo publishes a build base and a distroless runtime base image, so you can
containerise a bot without installing any native dependencies on the host. The
**[Deploy with Docker](../deploy/docker.md)** guide has a copyable two-stage
Dockerfile for the example bots and the run command (`-e DEEPGRAM_API_KEY=…`
etc., then open <http://localhost:8080>).

## Run locally

### Prerequisites

The default build is **cgo-free**: `go build ./...` needs no C toolchain. One
native library is loaded at run time, through
[purego](https://github.com/ebitengine/purego):

- **ONNX Runtime**: VAD and end-of-turn detection.

```sh
# Download a build for your platform, then point jargo at it:
export JARGO_ONNXRUNTIME_LIB=/path/to/libonnxruntime.so
```

Get it from the
[onnxruntime releases](https://github.com/microsoft/onnxruntime/releases); the
`onnxruntime-linux-*` archive contains `lib/libonnxruntime.so`. If the variable
is unset, jargo looks for the library by its conventional name on the loader's
default search path.

Without the ONNX Runtime the voice bot **still runs**: it falls back to STT
endpointing for turn-taking and loses barge-in. Everything else in the list
below is optional. See [Installation](installation.md) for RNNoise.

### Echo bot, no keys

```sh
go run ./examples/echo           # then open http://localhost:8080
```

### Voice bot

Set the provider API keys, then run:

```sh
export DEEPGRAM_API_KEY=...       # STT
export ANTHROPIC_API_KEY=...      # LLM
export ELEVENLABS_API_KEY=...     # TTS
go run ./examples/voicebot        # then open http://localhost:8080
```

The voicebot runs a fixed Deepgram + Anthropic + ElevenLabs stack. To try a
different provider, run one of the per-provider examples under
[`examples/voice`](../../examples/voice), one self-contained file each, with the
provider wired explicitly in Go:

```sh
go run ./examples/voice/cartesia     # Deepgram STT, Anthropic LLM, Cartesia TTS
go run ./examples/voice/openai       # OpenAI STT + LLM + TTS
go run ./examples/voice/groq         # Groq STT + LLM, ElevenLabs TTS
```

These are **headless backends**: they expose the WebRTC `/offer` endpoint and no
web UI. Point a browser client at `http://localhost:8080`, such as the
`nextjs-voicebot` example in
[jargo-client-react](https://github.com/gojargo/jargo-client-react), with
`NEXT_PUBLIC_JARGO_URL=http://localhost:8080`. Each example's doc comment lists
the API keys it needs.

### Custom voice

```sh
go run ./examples/voice/custom    # then open http://localhost:8080
```

This example serves an embedded UI at <http://localhost:8080>, unlike the other
`examples/voice/` backends. Settings live in `examples/voice/custom/.env`;
exported environment variables override the file. After changing config, restart
the process and reload the page.

To send 16 kHz mono PCM into STT:

```
STT_SAMPLE_RATE=16000
STT_CHANNELS=1
```

`STT_SAMPLE_RATE` must be one of `8000`, `12000`, `16000`, `24000`, or `48000`.
`STT_CHANNELS` must be `1`: the STT, VAD, and turn-taking path is mono, and any
other channel count fails at startup. This example requires the ONNX Runtime; it
does not fall back to STT endpointing. Transcription is per completed utterance
over HTTP, not a streaming STT session.

To use 44.1 kHz mono TTS PCM (the source format; playback stays 48 kHz):

```
TTS_SAMPLE_RATE=44100
TTS_CHANNELS=1
```

`TTS_SAMPLE_RATE` must be a positive integer; it defaults to `24000`.
`TTS_CHANNELS` must be `1` and defaults to `1`. The custom example is mono-only,
because the transport resampler does not downmix stereo to mono. The library's
TTS frame metadata can describe 1 or 2 channels; this example rejects 2 at
startup.

The TTS HTTP body is raw signed PCM16 little-endian at that rate and channel
count. `response_format` stays `pcm`; jargo does not decode WAV or negotiate a
format with the backend. STT stays independent at 16 kHz mono. Playback stays 48
kHz; the transport resamples TTS frames from their metadata. Do not change the
output clock to match the TTS source. After changing these values, restart the
process and reload the page.

## Next

- **[Your first bot](your-first-bot.md)**: the same pipeline, built up line by line.
- **[Architecture](../concepts/architecture.md)**: how the pieces fit together.
- **[Turn-taking](../guides/turn-taking.md)**: tuning end-of-turn detection and barge-in.
