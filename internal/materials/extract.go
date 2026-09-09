package materials

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const MaxUpload = 20 << 20
const MaxText = 200000
const maxXML = 8 << 20

func Extract(ctx context.Context, name string, data []byte) (string, error) {
	var text string
	var err error
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt":
		if !utf8.Valid(data) {
			return "", errors.New("Text files must use UTF-8 encoding")
		}
		text = string(data)
	case ".docx":
		text, err = extractDOCX(data)
	case ".pdf":
		text, err = extractPDF(ctx, data)
	default:
		return "", errors.New("Upload a DOCX, PDF, or UTF-8 TXT file")
	}
	if err != nil {
		return "", err
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\x00", "")
	if len(text) > MaxText {
		return "", errors.New("Too much text. Upload one lesson or a shorter document (up to 200 KB of text)")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("No readable text found. Scanned PDFs need OCR first; you can upload a text version")
	}
	return text, nil
}

func extractDOCX(data []byte) (string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", errors.New("This file is not a readable DOCX document")
	}
	var entry *zip.File
	for _, f := range archive.File {
		if f.Name == "word/document.xml" {
			entry = f
			break
		}
	}
	if entry == nil || entry.UncompressedSize64 > maxXML {
		return "", errors.New("DOCX contents are missing or too large")
	}
	reader, err := entry.Open()
	if err != nil {
		return "", errors.New("Could not read the Word document")
	}
	defer reader.Close()
	raw, err := io.ReadAll(io.LimitReader(reader, maxXML+1))
	if err != nil || len(raw) > maxXML {
		return "", errors.New("DOCX contents are too large or damaged")
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var out strings.Builder
	skip := 0
	inText := false
	const wordNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", errors.New("Word document XML is damaged")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space != wordNS {
				continue
			}
			if t.Name.Local == "del" {
				skip++
			}
			if skip > 0 {
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				out.WriteByte('\t')
			case "br", "cr":
				out.WriteByte('\n')
			}
		case xml.EndElement:
			if t.Name.Space != wordNS {
				continue
			}
			if t.Name.Local == "del" {
				skip--
				continue
			}
			if skip > 0 {
				continue
			}
			switch t.Name.Local {
			case "t":
				inText = false
			case "p", "tr":
				out.WriteByte('\n')
			case "tc":
				out.WriteByte('\t')
			}
		case xml.CharData:
			if inText && skip == 0 {
				out.Write(t)
			}
		}
		if out.Len() > MaxText {
			return "", errors.New("Extracted text is too large; upload a shorter lesson")
		}
	}
	return out.String(), nil
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > MaxText {
		return 0, errors.New("extracted text exceeds limit")
	}
	return b.Buffer.Write(p)
}
func extractPDF(ctx context.Context, data []byte) (string, error) {
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", errors.New("This file is not a PDF document")
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", errors.New("PDF extraction requires pdftotext (included in the Docker image)")
	}
	tmp, err := os.CreateTemp("", "romanian-source-*.pdf")
	if err != nil {
		return "", errors.New("Could not prepare PDF extraction")
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return "", errors.New("Could not prepare PDF extraction")
	}
	tmp.Close()
	command := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", tmp.Name(), "-")
	var out limitedOutput
	command.Stdout = &out
	if err = command.Run(); err != nil {
		return "", errors.New("Could not extract this PDF. It may be encrypted, damaged, too large, or require OCR")
	}
	return out.String(), nil
}
