package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/ui"
	"github.com/Pastranauwu/loom/internal/wrap"
)

func TestWrapCurlSintetizaVerificaYGuarda(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	gen := &generadorGuion{respuestas: []string{`{"name":"x","description":"d","tools":[{"name":"list_items","description":"lista","inputSchema":{"type":"object","properties":{"authorization":{"type":"string"}},"required":["authorization"]},"http":{"method":"GET","url":"https://api.ejemplo.dev/items","headers":{"Authorization":"{authorization}"}}}]}`}}
	m, err := runWrap(root, gen, wrapOpts{curl: `curl -H 'Authorization: Bearer abc' https://api.ejemplo.dev/items`, nombre: "items"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "items" || strings.Contains(gen.prompts[0], "Bearer abc") {
		t.Errorf("nombre %s · el secreto no viaja al modelo", m.Name)
	}
	if _, err := wrap.Load(root, "items"); err != nil {
		t.Fatalf("no se guardó: %v", err)
	}
}

func TestWrapRechazaYNoGuarda(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	gen := &generadorGuion{respuestas: []string{`{"name":"x","tools":[{"name":"run","inputSchema":{"type":"object","properties":{"c":{"type":"string"}},"required":["c"]},"exec":{"bin":"bash","args":["-c","{c}"]}}]}`}}
	if _, err := runWrap(root, gen, wrapOpts{curl: "curl https://h/x"}, false); err == nil || !strings.Contains(err.Error(), "bash") {
		t.Fatalf("la compuerta debía rechazar bash -c: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrap.Dir(root), "x.json")); err == nil {
		t.Error("un puente rechazado no se guarda")
	}
	if _, err := runWrap(root, nil, wrapOpts{cli: "a", curl: "b"}, false); err == nil {
		t.Error("dos fuentes a la vez: debe negarse")
	}
}
