// Package simplepdf writes a one page text PDF with the standard Helvetica
// font, which every reader has. Text is encoded as WinAnsi, so Portuguese
// accents work without embedding a font.
package simplepdf

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

const (
	pageWidth  = 595
	pageHeight = 842
	marginLeft = 50
	lineStep   = 18
	startY     = 790
)

type Line struct {
	Text string
	Bold bool
	Size int
}

func escape(text string) string {
	encoded, err := charmap.Windows1252.NewEncoder().String(text)
	if err != nil {
		encoded = text
	}
	return strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(encoded)
}

func Render(lines []Line) []byte {
	var content bytes.Buffer
	y := startY
	for _, line := range lines {
		size := line.Size
		if size == 0 {
			size = 11
		}
		font := "F1"
		if line.Bold {
			font = "F2"
		}
		fmt.Fprintf(&content, "BT /%s %d Tf %d %d Td (%s) Tj ET\n", font, size, marginLeft, y, escape(line.Text))
		y -= lineStep
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Resources << /Font << /F1 4 0 R /F2 5 0 R >> >> /Contents 6 0 R >>", pageWidth, pageHeight),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", content.Len(), content.String()),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects))
	for i, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}
