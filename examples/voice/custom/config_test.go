package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Isolate dotenv reads from the real configuration, including any symlink.
func setupConfig(t *testing.T, extra string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(envFile), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"LLM_BASE_URL", "LLM_MODEL", "LLM_API_KEY", "STT_BASE_URL", "STT_MODEL", "STT_API_KEY",
		"TTS_BASE_URL", "TTS_MODEL", "TTS_API_KEY", "TTS_VOICE",
		"STT_SAMPLE_RATE", "STT_CHANNELS", "TTS_SAMPLE_RATE", "TTS_CHANNELS",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	fixture := "LLM_BASE_URL=http://localhost:8000/v1\nLLM_MODEL=test\n" +
		"STT_BASE_URL=http://localhost:8001/v1\nSTT_MODEL=test\n" +
		"TTS_BASE_URL=http://localhost:8002/v1\nTTS_MODEL=test\nTTS_VOICE=test\n"
	if err := os.WriteFile(envFile, []byte(fixture+extra), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTTSFormatConfig(t *testing.T) {
	for _, tc := range []struct {
		name, dotenv string
		env          map[string]string
		rate         int
	}{
		{name: "defaults", rate: 24000},
		{name: "dotenv", dotenv: "TTS_SAMPLE_RATE=44100\nTTS_CHANNELS=1\n", rate: 44100},
		{
			name: "environment overrides dotenv", dotenv: "TTS_SAMPLE_RATE=24000\nTTS_CHANNELS=2\n",
			env: map[string]string{"TTS_SAMPLE_RATE": "44100", "TTS_CHANNELS": "1"}, rate: 44100,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupConfig(t, tc.dotenv)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.tts.SampleRate != tc.rate || cfg.tts.Channels != 1 {
				t.Errorf("TTS format = %d/%d, want %d/1", cfg.tts.SampleRate, cfg.tts.Channels, tc.rate)
			}
			if cfg.stt.SampleRate != 16000 || cfg.sttChannels != 1 {
				t.Errorf("STT format = %d/%d, want 16000/1", cfg.stt.SampleRate, cfg.sttChannels)
			}
		})
	}
}

func TestTTSFormatConfigRejectsInvalidValues(t *testing.T) {
	for _, key := range []string{"TTS_SAMPLE_RATE", "TTS_CHANNELS"} {
		values := []string{"", "invalid", "0", "-1", "1.5", "99999999999999999999999999"}
		if key == "TTS_CHANNELS" {
			values = append(values, "2", "3")
		}
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				setupConfig(t, "TTS_SAMPLE_RATE=44100\nTTS_CHANNELS=1\n")
				t.Setenv(key, value)
				_, err := loadConfig()
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Errorf("loadConfig() = %v, want %s error", err, key)
				}
			})
		}
	}
}
