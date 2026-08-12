package media

import (
	"bytes"
	"strings"
)

func mediaHeaderMatchesMIME(header []byte, claimedMIME string) bool {
	claimedMIME = strings.ToLower(strings.TrimSpace(claimedMIME))
	switch {
	case len(header) >= 3 && bytes.Equal(header[:3], []byte{0xff, 0xd8, 0xff}):
		return claimedMIME == "image/jpeg"
	case len(header) >= 8 && bytes.Equal(header[:8], []byte("\x89PNG\r\n\x1a\n")):
		return claimedMIME == "image/png"
	case len(header) >= 6 && (bytes.Equal(header[:6], []byte("GIF87a")) || bytes.Equal(header[:6], []byte("GIF89a"))):
		return claimedMIME == "image/gif"
	case len(header) >= 12 && bytes.Equal(header[:4], []byte("RIFF")) && bytes.Equal(header[8:12], []byte("WEBP")):
		return claimedMIME == "image/webp"
	case len(header) >= 4 && bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return claimedMIME == "video/webm"
	}
	brands, ok := isoBaseMediaBrands(header)
	if !ok {
		return false
	}
	hasBrand := func(allowed ...string) bool {
		for _, brand := range brands {
			for _, candidate := range allowed {
				if brand == candidate {
					return true
				}
			}
		}
		return false
	}
	switch claimedMIME {
	case "image/avif":
		return hasBrand("avif", "avis")
	case "image/heic", "image/heif":
		return hasBrand("heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1")
	case "video/quicktime":
		return hasBrand("qt  ")
	case "video/x-m4v":
		return hasBrand("M4V ", "M4VH", "M4VP")
	case "video/3gpp":
		for _, brand := range brands {
			if strings.HasPrefix(brand, "3gp") || strings.HasPrefix(brand, "3g2") {
				return true
			}
		}
		return false
	case "video/mp4":
		return hasBrand("isom", "iso2", "iso3", "iso4", "iso5", "iso6", "mp41", "mp42", "avc1", "dash", "MSNV", "M4V ", "M4VH", "M4VP")
	default:
		return false
	}
}

func isoBaseMediaBrands(header []byte) ([]string, bool) {
	if len(header) < 16 || !bytes.Equal(header[4:8], []byte("ftyp")) {
		return nil, false
	}
	boxSize := int(header[0])<<24 | int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	if boxSize < 16 {
		return nil, false
	}
	if boxSize > len(header) {
		boxSize = len(header)
	}
	brands := []string{string(header[8:12])}
	for offset := 16; offset+4 <= boxSize; offset += 4 {
		brands = append(brands, string(header[offset:offset+4]))
	}
	return brands, true
}
