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
