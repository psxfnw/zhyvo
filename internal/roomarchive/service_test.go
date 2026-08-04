package roomarchive

import "testing"

func TestArchiveFilename(t *testing.T) {
	if got := archiveFilename(` Wedding: Olena/Max `); got != "Wedding_ Olena_Max — фото.zip" {
		t.Fatalf("archiveFilename() = %q", got)
	}
}

func TestUniqueFilename(t *testing.T) {
	used := map[string]int{}
	if got := uniqueFilename("../IMG_1.JPG", used); got != "IMG_1.JPG" {
		t.Fatalf("first filename = %q", got)
	}
	if got := uniqueFilename("img_1.jpg", used); got != "img_1 (2).jpg" {
		t.Fatalf("duplicate filename = %q", got)
	}
}
