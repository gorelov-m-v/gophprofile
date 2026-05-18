package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	MimeJPEG = "image/jpeg"
	MimePNG  = "image/png"
	MimeWebP = "image/webp"

	MaxImageWidth  = 8192
	MaxImageHeight = 8192
	MaxImagePixels = 40_000_000
)

var supportedMimes = map[string]struct{}{
	MimeJPEG: {},
	MimePNG:  {},
	MimeWebP: {},
}

func SupportedFormats() string {
	return "Supported formats: jpeg, png, webp"
}

func DetectMime(data []byte) string {
	if len(data) >= 12 &&
		string(data[0:4]) == "RIFF" &&
		string(data[8:12]) == "WEBP" {
		return MimeWebP
	}
	return http.DetectContentType(data)
}

func Validate(data []byte, maxSize int64) (string, error) {
	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("file too large")
	}
	mime := DetectMime(data)
	if _, ok := supportedMimes[mime]; !ok {
		return "", fmt.Errorf("invalid file format")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode image config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > MaxImageWidth || cfg.Height > MaxImageHeight ||
		cfg.Width*cfg.Height > MaxImagePixels {
		return "", fmt.Errorf("image dimensions too large")
	}
	return mime, nil
}

func Dimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("decode image config: %w", err)
	}
	return cfg.Width, cfg.Height, nil
}

func ResizeJPEG(data []byte, width, height int) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	resized := imaging.Fill(img, width, height, imaging.Center, imaging.Lanczos)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 88}); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func Convert(data []byte, targetFormat string) ([]byte, string, error) {
	targetFormat = NormalizeFormat(targetFormat)
	if targetFormat == "" {
		return data, DetectMime(data), nil
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}

	var buf bytes.Buffer
	switch targetFormat {
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
			return nil, "", fmt.Errorf("encode jpeg: %w", err)
		}
		return buf.Bytes(), MimeJPEG, nil
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("encode png: %w", err)
		}
		return buf.Bytes(), MimePNG, nil
	case "webp":
		return nil, "", fmt.Errorf("webp output is not supported")
	default:
		return nil, "", fmt.Errorf("unsupported format")
	}
}

func MimeForFormat(format string) string {
	switch NormalizeFormat(format) {
	case "jpeg":
		return MimeJPEG
	case "png":
		return MimePNG
	case "webp":
		return MimeWebP
	default:
		return ""
	}
}

func NormalizeFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "":
		return ""
	case "jpg", "jpeg":
		return "jpeg"
	case "png":
		return "png"
	case "webp":
		return "webp"
	default:
		return "unsupported"
	}
}

func ExtensionForMime(mime string) string {
	switch strings.ToLower(mime) {
	case MimeJPEG:
		return ".jpg"
	case MimePNG:
		return ".png"
	case MimeWebP:
		return ".webp"
	default:
		return ".bin"
	}
}

func EncodePNG(w io.Writer, img image.Image) error {
	return png.Encode(w, img)
}
