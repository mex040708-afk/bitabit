package ingest

import (
	"errors"
	"testing"
)

func TestAACSampleRate(t *testing.T) {
	tests := map[int]int{
		22050:  22050,
		44100:  44100,
		48000:  48000,
		88200:  44100,
		96000:  48000,
		176400: 44100,
		192000: 48000,
		352800: 44100,
		384000: 48000,
	}
	for in, want := range tests {
		if got := aacSampleRate(in); got != want {
			t.Errorf("aacSampleRate(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPCMCodec(t *testing.T) {
	tests := map[int]string{
		12: "pcm_s16le",
		16: "pcm_s16le",
		20: "pcm_s24le",
		24: "pcm_s24le",
		32: "pcm_s32le",
	}
	for bits, want := range tests {
		if got, err := pcmCodec(bits); err != nil || got != want {
			t.Errorf("pcmCodec(%d) = %q, %v; want %q", bits, got, err, want)
		}
	}
	for _, bits := range []int{0, -1, 33, 64} {
		if _, err := pcmCodec(bits); !errors.Is(err, ErrUnreadable) {
			t.Errorf("pcmCodec(%d): error %v, want ErrUnreadable", bits, err)
		}
	}
}
