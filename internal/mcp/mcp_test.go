package mcp

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/wrap"
)

// puentes lee .loom/bridges como lo hace loom mcp: en cada petición.
func puentes(root string) func() []Tool {
	return func() []Tool {
		ms, _ := wrap.List(root)
		var tools []Tool
		for _, m := range ms {
			for _, t := range m.Tools {
				t := t
				tools = append(tools, Tool{Name: m.ToolName(t), Description: t.Description, Schema: t.InputSchema,
					Call: func(a map[string]any) (string, error) { return wrap.Invoke(t, a) }})
			}
		}
		return tools
	}
}

func TestPuentesDinamicosYEjecucionSegura(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("binario falso en shell")
	}
	root := t.TempDir()
	bin := filepath.Join(t.TempDir(), "eco-falso")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf 'n=%s args=%s' \"$#\" \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := wrap.Manifest{Name: "eco", Tools: []wrap.Tool{{Name: "say", Description: "repite",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"msg": map[string]any{"type": "string"}}, "required": []any{"msg"}},
		Exec:        &wrap.Exec{Bin: bin, Args: []string{"{msg}"}}}}}

	// la herramienta estática escribe el puente: el servidor ya corriendo lo ve
	estaticas := []Tool{{Name: "crear", Schema: map[string]any{"type": "object"}, Call: func(map[string]any) (string, error) {
		return "ok", wrap.Save(root, m)
	}}}
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"crear","arguments":{}}}` + "\n"
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out, "loom", "t", estaticas, puentes(root)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "notifications/tools/list_changed") {
		t.Errorf("crear un puente debe avisar al cliente:\n%s", out.String())
	}

	in = `{"jsonrpc":"2.0","id":1,"method":"initialize"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bridge_eco_say","arguments":{"msg":"hola; rm -rf ~"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"bridge_eco_say","arguments":{}}}
`
	out.Reset()
	if err := Serve(strings.NewReader(in), &out, "loom", "t", estaticas, puentes(root)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"listChanged":true`, `"name":"bridge_eco_say"`, `n=1 args=hola; rm -rf ~`, `"isError":true`} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en:\n%s", want, got)
		}
	}
	if strings.Contains(got, "list_changed") {
		t.Error("sin cambios no hay aviso")
	}
}

func TestDinamicaNoPisaEstatica(t *testing.T) {
	est := []Tool{{Name: "x", Call: func(map[string]any) (string, error) { return "estatica", nil }}}
	dyn := func() []Tool {
		return []Tool{{Name: "x", Call: func(map[string]any) (string, error) { return "dinamica", nil }}}
	}
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x"}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`
	var out bytes.Buffer
	_ = Serve(strings.NewReader(in), &out, "loom", "t", est, dyn)
	if !strings.Contains(out.String(), "estatica") || strings.Contains(out.String(), "dinamica") || strings.Count(out.String(), `"name":"x"`) != 1 {
		t.Errorf("got %s", out.String())
	}
}
