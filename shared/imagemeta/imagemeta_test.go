package imagemeta_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/shared/imagemeta"
)

func picture() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := range 40 {
		img.Set(x, x%30, color.RGBA{R: 200, A: 255})
	}
	return img
}

func exifSegment(orientation uint16) []byte {
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 2, 0}
	entry := func(tag, kind uint16, value uint32) []byte {
		out := make([]byte, 12)
		binary.LittleEndian.PutUint16(out[0:2], tag)
		binary.LittleEndian.PutUint16(out[2:4], kind)
		binary.LittleEndian.PutUint32(out[4:8], 1)
		binary.LittleEndian.PutUint32(out[8:12], value)
		return out
	}
	tiff = append(tiff, entry(0x0112, 3, uint32(orientation))...)
	tiff = append(tiff, entry(0x8825, 4, 0)...)
	tiff = append(tiff, 0, 0, 0, 0)
	tiff = append(tiff, []byte("GPSLatitude-8.8390")...)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	header := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(header[2:], uint16(len(payload)+2))
	return append(header, payload...)
}

func jpegWithExif(t *testing.T, orientation uint16) []byte {
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, picture(), nil))
	raw := encoded.Bytes()
	return append(append(append([]byte{}, raw[:2]...), exifSegment(orientation)...), raw[2:]...)
}

func TestJPEGLosesTheExifButKeepsTheOrientation(t *testing.T) {
	original := jpegWithExif(t, 6)
	require.Contains(t, string(original), "GPSLatitude")

	stripped, removed, err := imagemeta.Strip("image/jpeg", original)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NotContains(t, string(stripped), "GPSLatitude")
	assert.Contains(t, string(stripped), "Exif\x00\x00MM", "a minimal EXIF with the orientation only")

	_, err = jpeg.Decode(bytes.NewReader(stripped))
	require.NoError(t, err, "the picture still decodes")
	width, height, ok := imagemeta.Dimensions("image/jpeg", stripped)
	assert.True(t, ok)
	assert.Equal(t, 40, width)
	assert.Equal(t, 30, height)
}

func TestJPEGWithoutMetadataIsLeftAsItIs(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, jpeg.Encode(&encoded, picture(), nil))
	stripped, removed, err := imagemeta.Strip("image/jpeg", encoded.Bytes())
	require.NoError(t, err)
	assert.False(t, removed)
	assert.Equal(t, encoded.Bytes(), stripped)
}

func pngChunk(kind string, data []byte) []byte {
	out := make([]byte, 8, 12+len(data))
	binary.BigEndian.PutUint32(out[:4], uint32(len(data)))
	copy(out[4:], kind)
	out = append(out, data...)
	crc := make([]byte, 4)
	binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte(kind), data...)))
	return append(out, crc...)
}

func TestPNGLosesItsTextChunks(t *testing.T) {
	var encoded bytes.Buffer
	require.NoError(t, png.Encode(&encoded, picture()))
	raw := encoded.Bytes()
	iend := len(raw) - 12
	tagged := append(append(append([]byte{}, raw[:iend]...), pngChunk("tEXt", []byte("Location\x00Luanda"))...), raw[iend:]...)

	stripped, removed, err := imagemeta.Strip("image/png", tagged)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NotContains(t, string(stripped), "Luanda")
	_, err = png.Decode(bytes.NewReader(stripped))
	require.NoError(t, err)
}

func webp(chunks ...[]byte) []byte {
	body := []byte("WEBP")
	for _, chunk := range chunks {
		body = append(body, chunk...)
	}
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(body)))
	return append(append([]byte("RIFF"), size...), body...)
}

func riffChunk(kind string, data []byte) []byte {
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(len(data)))
	out := append(append([]byte(kind), size...), data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func TestWebPLosesExifAndXMP(t *testing.T) {
	header := []byte{webpFlags, 0, 0, 0, 39, 0, 0, 29, 0, 0}
	original := webp(riffChunk("VP8X", header), riffChunk("VP8L", []byte{0x2F, 0, 0, 0, 0}), riffChunk("EXIF", []byte("GPS-8.83")), riffChunk("XMP ", []byte("<x:xmpmeta/>")))

	stripped, removed, err := imagemeta.Strip("image/webp", original)
	require.NoError(t, err)
	assert.True(t, removed)
	assert.NotContains(t, string(stripped), "GPS")
	assert.NotContains(t, string(stripped), "xmpmeta")
	assert.Equal(t, byte(0x10), stripped[20], "the VP8X flags no longer announce EXIF or XMP")
	assert.Equal(t, uint32(len(stripped)-8), binary.LittleEndian.Uint32(stripped[4:8]))

	width, height, ok := imagemeta.Dimensions("image/webp", stripped)
	assert.True(t, ok)
	assert.Equal(t, 40, width)
	assert.Equal(t, 30, height)
}

const webpFlags = 0x10 | 0x08 | 0x04

func TestBrokenImagesAreRefused(t *testing.T) {
	for _, contentType := range []string{"image/jpeg", "image/png", "image/webp"} {
		_, _, err := imagemeta.Strip(contentType, []byte("not an image"))
		assert.ErrorIs(t, err, imagemeta.ErrMalformed, contentType)
	}
}

func TestHEICIsLeftUntouched(t *testing.T) {
	data := []byte("\x00\x00\x00\x18ftypheic")
	stripped, removed, err := imagemeta.Strip("image/heic", data)
	require.NoError(t, err)
	assert.False(t, removed)
	assert.Equal(t, data, stripped)
}
