package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestValidateDetectsPNGAndDimensions(t *testing.T) {
	data := pngBytes(t, 20, 10)

	mime, err := Validate(data, 1024*1024)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if mime != MimePNG {
		t.Fatalf("mime = %q", mime)
	}

	width, height, err := Dimensions(data)
	if err != nil {
		t.Fatalf("Dimensions() error = %v", err)
	}
	if width != 20 || height != 10 {
		t.Fatalf("dimensions = %dx%d", width, height)
	}
}

func TestValidateRejectsUnsupportedAndTooLarge(t *testing.T) {
	if _, err := Validate([]byte("hello"), 1024); err == nil {
		t.Fatal("expected unsupported file error")
	}
	if _, err := Validate(pngBytes(t, 4, 4), 2); err == nil {
		t.Fatal("expected too large error")
	}
}

func TestResizeJPEG(t *testing.T) {
	out, err := ResizeJPEG(pngBytes(t, 40, 30), 10, 10)
	if err != nil {
		t.Fatalf("ResizeJPEG() error = %v", err)
	}
	if DetectMime(out) != MimeJPEG {
		t.Fatalf("resized mime = %q", DetectMime(out))
	}
}

func TestFormatHelpers(t *testing.T) {
	if SupportedFormats() == "" {
		t.Fatal("SupportedFormats() is empty")
	}
	cases := map[string]string{
		MimeJPEG: ".jpg",
		MimePNG:  ".png",
		MimeWebP: ".webp",
		"other":  ".bin",
	}
	for mime, want := range cases {
		if got := ExtensionForMime(mime); got != want {
			t.Fatalf("ExtensionForMime(%q) = %q, want %q", mime, got, want)
		}
	}

	var buf bytes.Buffer
	if err := EncodePNG(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("EncodePNG() error = %v", err)
	}
	if DetectMime(buf.Bytes()) != MimePNG {
		t.Fatalf("encoded mime = %q", DetectMime(buf.Bytes()))
	}
	if MimeForFormat("jpg") != MimeJPEG || MimeForFormat("webp") != MimeWebP || MimeForFormat("bad") != "" {
		t.Fatalf("unexpected MimeForFormat results")
	}
}

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 80, B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
