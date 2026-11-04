package main

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestInstalarSimbolosNerdSoloLaMono(t *testing.T) {
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, n := range []string{"SymbolsNerdFont-Regular.ttf", "SymbolsNerdFontMono-Regular.ttf", "README.md"} {
		w, _ := z.Create(n)
		w.Write([]byte(n))
	}
	z.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	defer srv.Close()
	dir := t.TempDir()
	if err := instalarSimbolosNerd(srv.URL, dir); err != nil {
		t.Fatal(err)
	}
	fs, _ := os.ReadDir(dir)
	if len(fs) != 1 || fs[0].Name() != "SymbolsNerdFontMono-Regular.ttf" {
		t.Errorf("solo la Mono, que mide una celda: %v", fs)
	}
	malo := httptest.NewServer(http.NotFoundHandler())
	defer malo.Close()
	if instalarSimbolosNerd(malo.URL, filepath.Join(dir, "x")) == nil {
		t.Error("un 404 es error, no una fuente vacía")
	}
}
