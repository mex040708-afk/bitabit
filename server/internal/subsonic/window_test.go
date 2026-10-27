package subsonic

import (
	"net/http"
	"net/url"
	"testing"
)

func TestWindowClampsHugeOffsets(t *testing.T) {
	s := &server{log: quietLog()}
	form := url.Values{"size": {"20"}, "offset": {"9223372036854775807"}}
	req, err := http.NewRequest(http.MethodGet, "/rest/x?"+form.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := req.ParseForm(); err != nil {
		t.Fatal(err)
	}
	size, offset, bad := s.window(req, "size", "offset", defaultListSize)
	if bad != nil {
		t.Fatalf("window devolvió un error: %+v", bad.Error)
	}
	if offset != maxOffset || offset+size < 0 {
		t.Errorf("offset = %d, size = %d; want offset %d y sin desbordar", offset, size, maxOffset)
	}
	if got := slice([]int{1, 2, 3}, offset, size); got != nil {
		t.Errorf("slice con un offset enorme = %v; want nil", got)
	}
}
