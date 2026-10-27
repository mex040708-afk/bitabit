package library

import "testing"

func TestFile(t *testing.T) {
	tests := []struct {
		quality string
		id      int64
		ext     string
		want    string
	}{
		{"orig", 1, "flac", "orig/1.flac"},
		{"orig", 4312, "mp3", "orig/4312.mp3"},
		{"aac256", 4312, "m4a", "aac/4312.m4a"},
	}
	for _, tt := range tests {
		if got := File(tt.quality, tt.id, tt.ext); got != tt.want {
			t.Errorf("File(%q, %d, %q) = %q, want %q", tt.quality, tt.id, tt.ext, got, tt.want)
		}
	}
}

func TestCoverFile(t *testing.T) {
	tests := []struct {
		albumID int64
		ext     string
		want    string
	}{
		{1, "jpg", "covers/1.jpg"},
		{4312, "png", "covers/4312.png"},
	}
	for _, tt := range tests {
		if got := CoverFile(tt.albumID, tt.ext); got != tt.want {
			t.Errorf("CoverFile(%d, %q) = %q, want %q", tt.albumID, tt.ext, got, tt.want)
		}
	}
}
