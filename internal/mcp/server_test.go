package mcp

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeListaYLlama(t *testing.T) {
	tools := []Tool{{Name: "eco", Schema: map[string]any{"type": "object"},
		Call: func(a map[string]any) (string, error) { return "hola " + a["x"].(string), nil }}}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"eco","arguments":{"x":"a"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := Serve(strings.NewReader(in), &out, "loom", "t", tools); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{`"serverInfo"`, `"name":"eco"`, `hola a`, `-32601`} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %s en %s", want, got)
		}
	}
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("respuestas = %d, quiero 4 (la notificación no responde)", n)
	}
}
