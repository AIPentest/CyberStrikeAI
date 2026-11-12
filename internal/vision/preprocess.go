package vision

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"strings"

	"github.com/disintegration/imaging"
)

// ImagePayload The image bytes sent to the VL API and MIME.
type ImagePayload struct {
	Bytes    []byte
	MIMEType string
}

// PreprocessMeta Logs the resizing and encoding results, for tool output and troubleshooting.
type PreprocessMeta struct {
	OriginalPath   string
	OriginalBytes  int64
	OriginalWidth  int
	OriginalHeight int
	OutputWidth    int
	OutputHeight   int
	OutputBytes    int
	OutputMIMEType string
	JPEGQuality    int    // 0 indicates no JPEG re-encoding (the original image is passed through)
	PreprocessMode string // passthrough | jpeg
}

// PreprocessOptions Image-preprocessing parameters.
type PreprocessOptions struct {
	MaxImageBytes            int64
	MaxDimension             int
	JPEGQuality              int
	MaxPayloadBytes          int64
	SkipPreprocessBelowBytes int64 // 0 = Always compress; when >0, small images+If the size is compliant it can be passed through directly
}

// PreprocessImageFile Reads the image; large or oversized images go through imaging resizing+JPEG, Otherwise the original image can be passed through directly.
func PreprocessImageFile(path string, opt PreprocessOptions) (ImagePayload, PreprocessMeta, error) {
	var meta PreprocessMeta
	meta.OriginalPath = path

	st, err := os.Stat(path)
	if err != nil {
		return ImagePayload{}, meta, err
	}
	meta.OriginalBytes = st.Size()
	if opt.MaxImageBytes > 0 && st.Size() > opt.MaxImageBytes {
		return ImagePayload{}, meta, fmt.Errorf("file size %d exceeds max_image_bytes %d", st.Size(), opt.MaxImageBytes)
	}

	cfgW, cfgH, format, err := imageDimensions(path)
	if err != nil {
		return ImagePayload{}, meta, err
	}
	meta.OriginalWidth = cfgW
	meta.OriginalHeight = cfgH

	maxDim := opt.MaxDimension
	if maxDim <= 0 {
		maxDim = 2048
	}
	maxPayload := opt.MaxPayloadBytes
	if maxPayload <= 0 {
		maxPayload = 512 * 1024
	}

	if payload, meta, ok, err := tryPassthrough(path, st.Size(), cfgW, cfgH, format, opt, maxDim, maxPayload); ok {
		return payload, meta, err
	}

	return compressWithImaging(path, opt, maxDim, maxPayload, meta)
}

func tryPassthrough(path string, size int64, w, h int, format string, opt PreprocessOptions, maxDim int, maxPayload int64) (ImagePayload, PreprocessMeta, bool, error) {
	var meta PreprocessMeta
	meta.OriginalPath = path
	meta.OriginalBytes = size
	meta.OriginalWidth = w
	meta.OriginalHeight = h

	threshold := opt.SkipPreprocessBelowBytes
	if threshold <= 0 {
		return ImagePayload{}, meta, false, nil
	}
	if size > threshold {
		return ImagePayload{}, meta, false, nil
	}
	longEdge := w
	if h > longEdge {
		longEdge = h
	}
	if longEdge > maxDim {
		return ImagePayload{}, meta, false, nil
	}
	if size > maxPayload {
		return ImagePayload{}, meta, false, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return ImagePayload{}, meta, false, err
	}
	mime := mimeFromImageFormat(format)
	if mime == "" {
		return ImagePayload{}, meta, false, nil
	}

	meta.OutputWidth = w
	meta.OutputHeight = h
	meta.OutputBytes = len(raw)
	meta.OutputMIMEType = mime
	meta.PreprocessMode = "passthrough"
	return ImagePayload{Bytes: raw, MIMEType: mime}, meta, true, nil
}

func compressWithImaging(path string, opt PreprocessOptions, maxDim int, maxPayload int64, meta PreprocessMeta) (ImagePayload, PreprocessMeta, error) {
	src, err := imaging.Open(path)
	if err != nil {
		return ImagePayload{}, meta, fmt.Errorf("open image: %w", err)
	}
	bounds := src.Bounds()
	meta.OriginalWidth = bounds.Dx()
	meta.OriginalHeight = bounds.Dy()

	dst := imaging.Fit(src, maxDim, maxDim, imaging.Lanczos)
	outBounds := dst.Bounds()
	meta.OutputWidth = outBounds.Dx()
	meta.OutputHeight = outBounds.Dy()

	quality := opt.JPEGQuality
	if quality <= 0 || quality > 100 {
		quality = 82
	}

	dim := maxDim
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			dim = int(float64(dim) * 0.85)
			if dim < 256 {
				dim = 256
			}
			dst = imaging.Fit(src, dim, dim, imaging.Lanczos)
			outBounds = dst.Bounds()
			meta.OutputWidth = outBounds.Dx()
			meta.OutputHeight = outBounds.Dy()
		}
		q := quality
		for q >= 60 {
			var buf bytes.Buffer
			if err := imaging.Encode(&buf, dst, imaging.JPEG, imaging.JPEGQuality(q)); err != nil {
				return ImagePayload{}, meta, fmt.Errorf("encode jpeg: %w", err)
			}
			if int64(buf.Len()) <= maxPayload {
				meta.JPEGQuality = q
				meta.OutputBytes = buf.Len()
				meta.OutputMIMEType = "image/jpeg"
				meta.PreprocessMode = "jpeg"
				return ImagePayload{Bytes: buf.Bytes(), MIMEType: "image/jpeg"}, meta, nil
			}
			q -= 5
		}
		quality = 75
	}
	return ImagePayload{}, meta, fmt.Errorf("could not compress image under max_payload_bytes %d", maxPayload)
}

func imageDimensions(path string) (w, h int, format string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0, "", fmt.Errorf("decode image config: %w", err)
	}
	return cfg.Width, cfg.Height, format, nil
}

func mimeFromImageFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	case "tiff":
		return "image/tiff"
	default:
		return ""
	}
}

// DecodeImageConfig For testing: confirm the file can be decoded.
func DecodeImageConfig(path string) (image.Config, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, "", err
	}
	defer f.Close()
	return image.DecodeConfig(f)
}
