package filetype_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/shared/filetype"
)

func TestDetectReadsTheMagicBytes(t *testing.T) {
	cases := map[string][]byte{
		"image/jpeg":      {0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0},
		"image/png":       {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A},
		"image/webp":      []byte("RIFF\x00\x00\x00\x00WEBPVP8 "),
		"image/heic":      []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"),
		"application/pdf": []byte("%PDF-1.7\n"),
		"text/plain":      []byte("not an image at all"),
	}
	for expected, head := range cases {
		assert.Equal(t, expected, filetype.Detect(head), expected)
	}
}

func TestAnHEICNamedFileThatIsNotHEICIsNotTakenForOne(t *testing.T) {
	assert.NotEqual(t, "image/heic", filetype.Detect([]byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00")))
}

func TestExtensions(t *testing.T) {
	extension, known := filetype.Extension("image/heic")
	assert.True(t, known)
	assert.Equal(t, ".heic", extension)
	_, known = filetype.Extension("image/gif")
	assert.False(t, known)
	assert.True(t, filetype.IsKnownExtension(".pdf"))
	assert.False(t, filetype.IsKnownExtension(".exe"))
}
