package filetype

import (
	"bytes"
	"net/http"
	"strings"
)

const SniffBytes = 512

var extensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/webp":      ".webp",
	"image/heic":      ".heic",
	"application/pdf": ".pdf",
}

var heicBrands = [][]byte{[]byte("heic"), []byte("heix"), []byte("heim"), []byte("heis"), []byte("hevc"), []byte("hevx"), []byte("mif1"), []byte("msf1")}

func Detect(head []byte) string {
	if isHEIC(head) {
		return "image/heic"
	}
	return strings.TrimSpace(strings.Split(http.DetectContentType(head), ";")[0])
}

func isHEIC(head []byte) bool {
	if len(head) < 12 || !bytes.Equal(head[4:8], []byte("ftyp")) {
		return false
	}
	for _, brand := range heicBrands {
		if bytes.Equal(head[8:12], brand) {
			return true
		}
	}
	return false
}

func Extension(contentType string) (string, bool) {
	extension, known := extensions[contentType]
	return extension, known
}

func IsKnownExtension(extension string) bool {
	for _, known := range extensions {
		if known == extension {
			return true
		}
	}
	return false
}
