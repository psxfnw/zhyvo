package media

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateInitiate(t *testing.T) {
	tests := []struct {
		name      string
		input     InitiateInput
		wantType  string
		wantMIME  string
		wantError bool
	}{
		{name: "jpeg", input: InitiateInput{Filename: "photo.jpg", MIMEType: "image/jpeg", SizeBytes: 1024}, wantType: "image", wantMIME: "image/jpeg"},
		{name: "mime parameters normalized", input: InitiateInput{Filename: "clip.mp4", MIMEType: "Video/MP4; charset=binary", SizeBytes: 1024}, wantType: "video", wantMIME: "video/mp4"},
		{name: "empty filename", input: InitiateInput{MIMEType: "image/jpeg", SizeBytes: 1024}, wantError: true},
		{name: "control character", input: InitiateInput{Filename: "bad\nname.jpg", MIMEType: "image/jpeg", SizeBytes: 1024}, wantError: true},
		{name: "unsupported type", input: InitiateInput{Filename: "file.exe", MIMEType: "application/octet-stream", SizeBytes: 1024}, wantError: true},
		{name: "oversized image", input: InitiateInput{Filename: "huge.jpg", MIMEType: "image/jpeg", SizeBytes: maxImageSize + 1}, wantError: true},
		{name: "oversized video", input: InitiateInput{Filename: "huge.mp4", MIMEType: "video/mp4", SizeBytes: maxVideoSize + 1}, wantError: true},
		{name: "valid checksum", input: InitiateInput{Filename: "photo.jpg", MIMEType: "image/jpeg", SizeBytes: 1024, Checksum: strings.Repeat("A1", 32)}, wantType: "image", wantMIME: "image/jpeg"},
		{name: "invalid checksum", input: InitiateInput{Filename: "photo.jpg", MIMEType: "image/jpeg", SizeBytes: 1024, Checksum: "not-sha256"}, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mediaType, normalized, err := validateInitiate(test.input)
			if test.wantError {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("validateInitiate() error = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateInitiate() error = %v", err)
			}
			if mediaType != test.wantType || normalized.MIMEType != test.wantMIME {
				t.Fatalf("validateInitiate() = (%q, %q), want (%q, %q)", mediaType, normalized.MIMEType, test.wantType, test.wantMIME)
			}
			if test.name == "valid checksum" && normalized.Checksum != strings.ToLower(test.input.Checksum) {
				t.Fatalf("checksum was not normalized: %q", normalized.Checksum)
			}
		})
	}
}

func TestValidateCompletedParts(t *testing.T) {
	expected := 2
	parts, err := validateCompletedParts([]CompletedPart{
		{PartNumber: 2, ETag: "etag-2"},
		{PartNumber: 1, ETag: "etag-1"},
	}, &expected)
	if err != nil {
		t.Fatalf("validateCompletedParts() error = %v", err)
	}
	if parts[0].PartNumber != 1 || parts[1].PartNumber != 2 {
		t.Fatalf("validateCompletedParts() did not sort parts: %#v", parts)
	}

	_, err = validateCompletedParts([]CompletedPart{{PartNumber: 1, ETag: "etag-1"}}, &expected)
	if !errors.Is(err, ErrInvalidUploadParts) {
		t.Fatalf("validateCompletedParts() error = %v, want ErrInvalidUploadParts", err)
	}
}
