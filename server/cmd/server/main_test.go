package main

import (
	"testing"

	"github.com/mex040708-afk/bitabit/server/internal/stream"
)

func TestParseLinkArgs(t *testing.T) {
	good := []struct {
		args    []string
		id      int64
		quality stream.Quality
	}{
		{[]string{"1"}, 1, stream.QualityOriginal},
		{[]string{"4312", "aac256"}, 4312, stream.QualityAAC256},
		{[]string{"7", "orig"}, 7, stream.QualityOriginal},
	}
	for _, tt := range good {
		id, q, err := parseLinkArgs(tt.args)
		if err != nil || id != tt.id || q != tt.quality {
			t.Errorf("parseLinkArgs(%q) = %d, %q, %v", tt.args, id, q, err)
		}
	}
	for _, args := range [][]string{nil, {"0"}, {"-1"}, {"uno"}, {"1", "hires"}, {"1", "orig", "más"}} {
		if _, _, err := parseLinkArgs(args); err == nil {
			t.Errorf("parseLinkArgs(%q) no falla", args)
		}
	}
}
