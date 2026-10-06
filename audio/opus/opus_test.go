package opus_test

import (
	"encoding/binary"
	"math"
	"strconv"
	"testing"

	"github.com/gojargo/jargo/audio/opus"
)

// sineFrame builds one 20 ms mono frame of S16LE PCM at the given frequency.
func sineFrame(freq float64) []byte {
	pcm := make([]byte, opus.FrameBytes(1))
	for i := range opus.FrameSamples {
		v := math.Sin(2*math.Pi*freq*float64(i)/float64(opus.SampleRate)) * 0.3 * math.MaxInt16
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(v)))
	}
	return pcm
}

func TestEncodeProducesPacket(t *testing.T) {
	enc, err := opus.NewEncoder(opus.EncoderConfig{Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.Encode(sineFrame(440))
	if err != nil {
		t.Fatal(err)
	}
	if len(packet) == 0 {
		t.Fatal("encoded packet is empty")
	}
}

func TestEncodeRejectsWrongFrameSize(t *testing.T) {
	enc, err := opus.NewEncoder(opus.EncoderConfig{Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(make([]byte, opus.FrameBytes(1)-2)); err == nil {
		t.Fatal("expected an error for a short frame")
	}
}

func TestRoundTrip(t *testing.T) {
	enc, err := opus.NewEncoder(opus.EncoderConfig{Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := opus.NewDecoder(1)
	if err != nil {
		t.Fatal(err)
	}

	packet, err := enc.Encode(sineFrame(440))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := dec.Decode(packet)
	if err != nil {
		t.Fatal(err)
	}
	// A 20 ms frame decodes to 960 mono samples (1920 bytes).
	if got, want := len(pcm), opus.FrameBytes(1); got != want {
		t.Fatalf("decoded PCM = %d bytes, want %d", got, want)
	}
}

func TestDecoderOutputFormat(t *testing.T) {
	enc, err := opus.NewEncoder(opus.EncoderConfig{Channels: 1})
	if err != nil {
		t.Fatal(err)
	}
	packet, err := enc.Encode(sineFrame(440))
	if err != nil {
		t.Fatal(err)
	}
	for _, rate := range []int{8000, 12000, 16000, 24000, 48000} {
		for _, channels := range []int{1, 2} {
			t.Run(strconv.Itoa(rate)+"/"+strconv.Itoa(channels), func(t *testing.T) {
				dec, err := opus.NewDecoderWithOutput(rate, channels)
				if err != nil {
					t.Fatal(err)
				}
				pcm, err := dec.Decode(packet)
				if err != nil {
					t.Fatal(err)
				}
				if got, want := len(pcm), rate/50*channels*2; got != want {
					t.Fatalf("decoded PCM = %d bytes, want %d", got, want)
				}
			})
		}
	}
}

func TestDecoderRejectsUnsupportedFormat(t *testing.T) {
	for _, format := range [][2]int{{0, 1}, {44100, 1}, {16000, 0}, {16000, 3}} {
		if _, err := opus.NewDecoderWithOutput(format[0], format[1]); err == nil {
			t.Errorf("NewDecoderWithOutput(%d, %d): want error", format[0], format[1])
		}
	}
}
