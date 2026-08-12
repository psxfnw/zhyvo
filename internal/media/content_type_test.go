package media

import "testing"

func TestMediaHeaderMatchesMIME(t *testing.T) {
	iso := func(major string, compatible ...string) []byte {
		size := 16 + len(compatible)*4
		result := []byte{byte(size >> 24), byte(size >> 16), byte(size >> 8), byte(size), 'f', 't', 'y', 'p'}
		result = append(result, []byte(major)...)
		result = append(result, 0, 0, 0, 0)
		for _, brand := range compatible {
			result = append(result, []byte(brand)...)
		}
		return result
	}
	tests := []struct {
		name   string
		header []byte
		mime   string
		want   bool
	}{
		{name: "jpeg", header: []byte{0xff, 0xd8, 0xff, 0xe0}, mime: "image/jpeg", want: true},
		{name: "png", header: []byte("\x89PNG\r\n\x1a\nrest"), mime: "image/png", want: true},
		{name: "gif", header: []byte("GIF89a"), mime: "image/gif", want: true},
		{name: "webp", header: []byte("RIFF0000WEBP"), mime: "image/webp", want: true},
		{name: "webm", header: []byte{0x1a, 0x45, 0xdf, 0xa3}, mime: "video/webm", want: true},
		{name: "avif compatible brand", header: iso("mif1", "avif"), mime: "image/avif", want: true},
		{name: "heic", header: iso("heic"), mime: "image/heic", want: true},
		{name: "mp4", header: iso("isom", "mp42"), mime: "video/mp4", want: true},
		{name: "mov", header: iso("qt  "), mime: "video/quicktime", want: true},
		{name: "three gp", header: iso("3gp6"), mime: "video/3gpp", want: true},
		{name: "executable disguised as jpeg", header: []byte("MZ executable"), mime: "image/jpeg", want: false},
		{name: "jpeg declared png", header: []byte{0xff, 0xd8, 0xff, 0xe0}, mime: "image/png", want: false},
		{name: "avif declared mp4", header: iso("avif"), mime: "video/mp4", want: false},
		{name: "truncated iso", header: []byte{0, 0, 0, 12, 'f', 't', 'y', 'p'}, mime: "video/mp4", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mediaHeaderMatchesMIME(test.header, test.mime); got != test.want {
				t.Fatalf("mediaHeaderMatchesMIME() = %v, want %v", got, test.want)
			}
		})
	}
}
