package thumbnail

import (
	"strings"
	"testing"
)

func TestParseFFprobeAppliesRotationAndDuration(t *testing.T) {
	probe, err := parseFFprobe([]byte(`{"streams":[{"width":1920,"height":1080,"duration":"N/A","tags":{},"side_data_list":[{"rotation":-90}]}],"format":{"duration":"12.345"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if probe.Width != 1080 || probe.Height != 1920 || probe.DurationMS != 12345 {
		t.Fatalf("unexpected probe: %+v", probe)
	}
}

func TestThumbnailSeek(t *testing.T) {
	for _, test := range []struct {
		duration int64
		want     string
	}{{0, "1"}, {500, "0.10"}, {10_000, "1.00"}, {120_000, "3.00"}} {
		if got := thumbnailSeek(test.duration); got != test.want {
			t.Fatalf("thumbnailSeek(%d) = %q, want %q", test.duration, got, test.want)
		}
	}
}

func TestSanitizeCommandErrorRemovesSignedURL(t *testing.T) {
	source := "https://storage.invalid/file?X-Amz-Signature=secret"
	err := sanitizeCommandError(assertError(source), source)
	if strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "[media source]") {
		t.Fatalf("URL was not sanitized: %v", err)
	}
}

type assertError string

func (err assertError) Error() string { return string(err) }
