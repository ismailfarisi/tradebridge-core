package attachments

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSniffAndAllowed(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 64)...)
	if m := Sniff(png); m != "image/png" {
		t.Fatalf("png sniff = %q", m)
	}
	pdf := append([]byte("%PDF-1.4\n"), make([]byte, 64)...)
	if m := Sniff(pdf); m != "application/pdf" {
		t.Fatalf("pdf sniff = %q", m)
	}
	if !Allowed("image/png") || !Allowed("application/pdf") {
		t.Fatal("png/pdf should be allowed")
	}
	if Allowed("text/html") || Allowed("application/zip") {
		t.Fatal("html/zip must not be allowed")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Put("att1", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("att1")
	if err != nil || !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("get = %q, %v", got, err)
	}
	if err := s.Delete("att1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("att1"); err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestContentDisposition(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{
			name:     "plain name",
			filename: "invoice.pdf",
			want:     `inline; filename="invoice.pdf"; filename*=UTF-8''invoice.pdf`,
		},
		{
			name:     "name with double quote",
			filename: `weird"name.pdf`,
			want:     `inline; filename="weird\"name.pdf"; filename*=UTF-8''weird%22name.pdf`,
		},
		{
			name:     "name with backslash",
			filename: `weird\name.pdf`,
			want:     `inline; filename="weird\\name.pdf"; filename*=UTF-8''weird%5Cname.pdf`,
		},
		{
			name:     "unicode name",
			filename: "café résumé.pdf",
			want:     `inline; filename="café résumé.pdf"; filename*=UTF-8''caf%C3%A9%20r%C3%A9sum%C3%A9.pdf`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ContentDisposition(tc.filename)
			if got != tc.want {
				t.Fatalf("ContentDisposition(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestResolveDir(t *testing.T) {
	if d, err := ResolveDir("/x/y"); err != nil || d != "/x/y" {
		t.Fatalf("configured dir: %q, %v", d, err)
	}
	home, _ := os.UserHomeDir()
	d, err := ResolveDir("")
	if err != nil || d != filepath.Join(home, "tradebridge-data", "attachments") {
		t.Fatalf("default dir: %q, %v", d, err)
	}
}
