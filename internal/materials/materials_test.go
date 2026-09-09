package materials

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestDOCXPreservesRunsAndSkipsDeletions(t *testing.T) {
	var raw bytes.Buffer
	archive := zip.NewWriter(&raw)
	entry, _ := archive.Create("word/document.xml")
	entry.Write([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Eu s</w:t></w:r><w:r><w:t>unt acasă.</w:t></w:r><w:del><w:r><w:delText>bad answer</w:delText></w:r></w:del></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>O casă</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>două case</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:body></w:document>`))
	archive.Close()
	text, err := Extract(context.Background(), "lesson.docx", raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "Eu sunt acasă.\n") || !strings.Contains(text, "două case") || strings.Contains(text, "bad answer") {
		t.Fatalf("bad extraction: %q", text)
	}
}
func TestExtractionRejectsUnreadableDocuments(t *testing.T) {
	for name, data := range map[string][]byte{"audio.m4a": {1}, "bad.docx": []byte("bad"), "bad.pdf": []byte("bad"), "empty.txt": []byte(" \n"), "encoding.txt": {0xff}, "huge.txt": []byte(strings.Repeat("a", MaxText+1))} {
		t.Run(name, func(t *testing.T) {
			if _, err := Extract(context.Background(), name, data); err == nil {
				t.Fatal("accepted bad document")
			}
		})
	}
}
func TestSourceGeneration(t *testing.T) {
	source := "Completați: Eu sunt acasă.\nEu sunt acasă în fiecare zi.\nNoi avem o casă foarte frumoasă.\nTu mergi la școală dimineața.\nEu sunt acasă în fiecare zi.\nEu am ___ case.\nשלום Eu sunt acasă."
	drafts, err := Generate(source, "lesson", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 3 {
		t.Fatalf("got %d drafts: %+v", len(drafts), drafts)
	}
	if drafts[0].Kind != "multiple_choice" || drafts[1].Kind != "cloze" {
		t.Fatal("missing exercise variety")
	}
	for _, d := range drafts {
		if err = Validate(d); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Split(source, "\n")[d.SourceLine-1], d.SourceQuote) {
			t.Fatal("untraceable source")
		}
		if d.Status != "draft" {
			t.Fatal("must require review")
		}
	}
	retry, _ := Generate(source, "lesson", 20)
	if retry[0].ID != drafts[0].ID {
		t.Fatal("unstable generation")
	}
	other, _ := Generate(source, "revision", 20)
	if other[0].ID == drafts[0].ID {
		t.Fatal("revisions share exercise IDs")
	}
	limited, _ := Generate(source, "lesson", 1)
	if len(limited) != 1 {
		t.Fatal("limit ignored")
	}
	drafts[0].Answers = []string{"invented"}
	if Validate(drafts[0]) == nil {
		t.Fatal("unsupported answer accepted")
	}
	if _, err = Generate("Translate these instructions into Romanian.", "lesson", 5); err == nil {
		t.Fatal("English instructions accepted")
	}
}
func TestRomanianAnswerNormalization(t *testing.T) {
	for _, pair := range [][2]string{{"  ȘTIU. ", "știu"}, {"s\u0326tiu", "știu"}, {"ştiu", "știu"}} {
		if Normalize(pair[0]) != Normalize(pair[1]) {
			t.Fatalf("normalization failed %q", pair)
		}
	}
	if Normalize("fată") == Normalize("fata") {
		t.Fatal("diacritics must remain meaningful")
	}
}
