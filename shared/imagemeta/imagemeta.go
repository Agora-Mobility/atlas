package imagemeta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
)

var ErrMalformed = errors.New("the image is malformed")

const (
	contentJPEG = "image/jpeg"
	contentPNG  = "image/png"
	contentWebP = "image/webp"

	markerPrefix   = 0xFF
	markerSOI      = 0xD8
	markerSOS      = 0xDA
	markerEOI      = 0xD9
	markerAPP1     = 0xE1
	markerAPP13    = 0xED
	markerCOM      = 0xFE
	orientationTag = 0x0112
	webpEXIFFlag   = 0x08
	webpXMPFlag    = 0x04
)

func Strip(contentType string, data []byte) ([]byte, bool, error) {
	switch contentType {
	case contentJPEG:
		return stripJPEG(data)
	case contentPNG:
		return stripPNG(data)
	case contentWebP:
		return stripWebP(data)
	}
	return data, false, nil
}

func Dimensions(contentType string, data []byte) (int, int, bool) {
	switch contentType {
	case contentJPEG:
		return configSize(jpeg.DecodeConfig(bytes.NewReader(data)))
	case contentPNG:
		return configSize(png.DecodeConfig(bytes.NewReader(data)))
	case contentWebP:
		return webpDimensions(data)
	}
	return 0, 0, false
}

func configSize(config image.Config, err error) (int, int, bool) {
	if err != nil {
		return 0, 0, false
	}
	return config.Width, config.Height, true
}

func isStandalone(marker byte) bool {
	return marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7)
}

func stripJPEG(data []byte) ([]byte, bool, error) {
	if len(data) < 4 || data[0] != markerPrefix || data[1] != markerSOI {
		return nil, false, ErrMalformed
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)))
	out.Write(data[:2])
	removed, orientation := false, uint16(0)
	position := 2
	for position < len(data) {
		if data[position] != markerPrefix || position+1 >= len(data) {
			return nil, false, ErrMalformed
		}
		marker := data[position+1]
		if marker == markerPrefix {
			position++
			continue
		}
		if isStandalone(marker) {
			out.Write(data[position : position+2])
			position += 2
			continue
		}
		if marker == markerEOI {
			out.Write(data[position:])
			break
		}
		if position+4 > len(data) {
			return nil, false, ErrMalformed
		}
		length := int(binary.BigEndian.Uint16(data[position+2 : position+4]))
		end := position + 2 + length
		if length < 2 || end > len(data) {
			return nil, false, ErrMalformed
		}
		if marker == markerSOS {
			if orientation > 1 {
				writeOrientation(out, orientation)
			}
			out.Write(data[position:])
			break
		}
		segment := data[position:end]
		switch marker {
		case markerAPP1, markerAPP13, markerCOM:
			if marker == markerAPP1 && orientation == 0 {
				orientation = exifOrientation(segment[4:])
			}
			removed = true
		default:
			out.Write(segment)
		}
		position = end
	}
	return out.Bytes(), removed, nil
}

func exifOrientation(payload []byte) uint16 {
	if len(payload) < 14 || !bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
		return 0
	}
	tiff := payload[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	offset := int(order.Uint32(tiff[4:8]))
	if offset+2 > len(tiff) {
		return 0
	}
	entries := int(order.Uint16(tiff[offset : offset+2]))
	for i := range entries {
		start := offset + 2 + i*12
		if start+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[start:start+2]) == orientationTag {
			value := order.Uint16(tiff[start+8 : start+10])
			if value >= 1 && value <= 8 {
				return value
			}
			return 0
		}
	}
	return 0
}

func writeOrientation(out *bytes.Buffer, orientation uint16) {
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1}
	entry := make([]byte, 12)
	binary.BigEndian.PutUint16(entry[0:2], orientationTag)
	binary.BigEndian.PutUint16(entry[2:4], 3)
	binary.BigEndian.PutUint32(entry[4:8], 1)
	binary.BigEndian.PutUint16(entry[8:10], orientation)
	payload := append(append(append([]byte("Exif\x00\x00"), tiff...), entry...), 0, 0, 0, 0)
	header := []byte{markerPrefix, markerAPP1, 0, 0}
	binary.BigEndian.PutUint16(header[2:4], uint16(len(payload)+2)) // #nosec G115 -- a fixed 32-byte payload
	out.Write(header)
	out.Write(payload)
}

var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

var pngMetadataChunks = map[string]bool{"eXIf": true, "tEXt": true, "zTXt": true, "iTXt": true, "tIME": true}

func stripPNG(data []byte) ([]byte, bool, error) {
	if len(data) < len(pngSignature) || !bytes.Equal(data[:len(pngSignature)], pngSignature) {
		return nil, false, ErrMalformed
	}
	out := bytes.NewBuffer(make([]byte, 0, len(data)))
	out.Write(pngSignature)
	removed := false
	position := len(pngSignature)
	for position < len(data) {
		if position+8 > len(data) {
			return nil, false, ErrMalformed
		}
		length := int(binary.BigEndian.Uint32(data[position : position+4]))
		end := position + 12 + length
		if length < 0 || end > len(data) {
			return nil, false, ErrMalformed
		}
		kind := string(data[position+4 : position+8])
		if pngMetadataChunks[kind] {
			removed = true
		} else {
			out.Write(data[position:end])
		}
		position = end
		if kind == "IEND" {
			break
		}
	}
	return out.Bytes(), removed, nil
}

func stripWebP(data []byte) ([]byte, bool, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return nil, false, ErrMalformed
	}
	body := bytes.NewBuffer(make([]byte, 0, len(data)))
	removed := false
	position := 12
	for position+8 <= len(data) {
		kind := string(data[position : position+4])
		size := int(binary.LittleEndian.Uint32(data[position+4 : position+8]))
		end := position + 8 + size + size%2
		if size < 0 || position+8+size > len(data) {
			return nil, false, ErrMalformed
		}
		end = min(end, len(data))
		chunk := data[position:end]
		switch kind {
		case "EXIF", "XMP ":
			removed = true
		case "VP8X":
			copied := bytes.Clone(chunk)
			if len(copied) > 8 {
				copied[8] &^= webpEXIFFlag | webpXMPFlag
			}
			body.Write(copied)
		default:
			body.Write(chunk)
		}
		position = end
	}
	out := bytes.NewBuffer(make([]byte, 0, body.Len()+12))
	out.WriteString("RIFF")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(body.Len()+4)) // #nosec G115 -- bounded by the file size limit
	out.Write(size)
	out.WriteString("WEBP")
	out.Write(body.Bytes())
	return out.Bytes(), removed, nil
}

func webpDimensions(data []byte) (int, int, bool) {
	if len(data) < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, false
	}
	chunk := data[12:]
	switch string(chunk[:4]) {
	case "VP8X":
		width := 1 + (int(chunk[12]) | int(chunk[13])<<8 | int(chunk[14])<<16)
		height := 1 + (int(chunk[15]) | int(chunk[16])<<8 | int(chunk[17])<<16)
		return width, height, true
	case "VP8 ":
		width := int(binary.LittleEndian.Uint16(chunk[14:16]) & 0x3FFF)
		height := int(binary.LittleEndian.Uint16(chunk[16:18]) & 0x3FFF)
		return width, height, true
	case "VP8L":
		bits := binary.LittleEndian.Uint32(chunk[9:13])
		return int(bits&0x3FFF) + 1, int((bits>>14)&0x3FFF) + 1, true
	}
	return 0, 0, false
}
