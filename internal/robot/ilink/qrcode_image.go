package ilink

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

// QRCodeDataURL Encodes the QR-code content (usually a liteapp link) as a PNG data URL for display on the web side.
// qrcode_img_content is not a direct image link and cannot be used as <img src>.
func QRCodeDataURL(content string, size int) (string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty qr content")
	}
	if size <= 0 {
		size = 256
	}
	png, err := qrcode.Encode(content, qrcode.Medium, size)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
