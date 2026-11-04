package simplepdf_test

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/shared/simplepdf"
)

func TestRenderIsAValidOnePagePDF(t *testing.T) {
	out := simplepdf.Render([]simplepdf.Line{{Text: "Recibo (TX-1)", Bold: true, Size: 16}, {Text: "Tarifa do serviço: 1 000,00 Kz"}})

	assert.True(t, bytes.HasPrefix(out, []byte("%PDF-1.4")))
	assert.True(t, bytes.HasSuffix(out, []byte("%%EOF\n")))
	assert.Contains(t, string(out), `Recibo \(TX-1\)`, "parentheses are escaped")
	assert.Contains(t, string(out), "servi\xe7o", "accents in WinAnsi")

	marker := bytes.LastIndex(out, []byte("startxref\n"))
	require.Positive(t, marker)
	rest := out[marker+len("startxref\n"):]
	offset, err := strconv.Atoi(string(rest[:bytes.IndexByte(rest, '\n')]))
	require.NoError(t, err)
	assert.Equal(t, "xref", string(out[offset:offset+4]), "the table of objects is where the trailer says")
}
