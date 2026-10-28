package wrap

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("los binarios falsos son scripts de shell")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCurlConHeadersYCuerpoAManifiesto(t *testing.T) {
	r, err := ParseCurl(`curl 'https://api.ejemplo.dev/v1/items?limit=5' \
  -H 'Authorization: Bearer secreto123' -H "Content-Type: application/json" \
  --data-raw '{"name":"a","qty":2}'`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "POST" || r.URL != "https://api.ejemplo.dev/v1/items?limit=5" {
		t.Fatalf("request = %+v", r)
	}
	if r.Headers["Authorization"] != "{authorization}" || r.Headers["Content-Type"] != "application/json" {
		t.Errorf("headers = %v · el secreto se reemplaza por una variable", r.Headers)
	}
	if shape, _ := r.BodyShape.(map[string]any); shape["qty"] != "number" || shape["name"] != "string" {
		t.Errorf("forma del cuerpo = %v", r.BodyShape)
	}
	p := Prompt("api", "items", Resumen(r))
	if strings.Contains(p, "secreto123") || !strings.Contains(p, "{authorization}") {
		t.Error("el prompt no debe llevar el secreto")
	}

	respuesta := "Listo:\n```json\n" + `{"name":"x","description":"items api","tools":[{"name":"create_item","description":"crea","inputSchema":{"type":"object","properties":{"authorization":{"type":"string"},"name":{"type":"string"},"qty":{"type":"integer"}},"required":["authorization","name","qty"]},"http":{"method":"POST","url":"https://api.ejemplo.dev/v1/items","headers":{"Authorization":"{authorization}","Content-Type":"application/json"},"body":"{\"name\":\"{name}\",\"qty\":{qty}}"}}]}` + "\n```"
	m, err := Parse(respuesta, "items")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "items" {
		t.Errorf("--name manda sobre el del modelo: %s", m.Name)
	}
	if _, err := Verify(m, false); err != nil {
		t.Fatalf("manifiesto válido rechazado: %v", err)
	}
}

func TestVerifyRechazaShellYVariablesSueltas(t *testing.T) {
	esq := map[string]any{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string"}}, "required": []any{"cmd"}}
	casos := map[string]Tool{
		"sh -c":      {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "sh", Args: []string{"-c", "{cmd}"}}},
		"bash -c":    {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "/bin/bash", Args: []string{"-c", "{cmd}"}}},
		"env":        {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "env", Args: []string{"{cmd}"}}},
		"metachar":   {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "ls;rm", Args: []string{"{cmd}"}}},
		"bin en var": {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "{cmd}"}},
		"sin prop":   {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "ls", Args: []string{"{otro}"}}},
		"no requerida": {Name: "t", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"cmd": map[string]any{"type": "string"}}},
			Exec: &Exec{Bin: "ls", Args: []string{"{cmd}"}}},
		"exec y http": {Name: "t", InputSchema: esq, Exec: &Exec{Bin: "ls"}, HTTP: &HTTP{Method: "GET", URL: "https://x"}},
		"file url":    {Name: "t", InputSchema: esq, HTTP: &HTTP{Method: "GET", URL: "file:///etc/passwd"}},
	}
	for nombre, tool := range casos {
		if _, err := Verify(Manifest{Name: "p", Tools: []Tool{tool}}, false); err == nil {
			t.Errorf("%s: la compuerta debía rechazarlo", nombre)
		}
	}
}

func TestHelpSimuladoAHerramientasEInvocacionSegura(t *testing.T) {
	fakeBin(t, "herramientafalsa", `case "$1" in
--help) echo "usage: herramientafalsa <cmd>"; echo "  greet NAME   says hello"; exit 2;;
--version) echo "1.0";;
greet) printf '%s|%s' "$2" "$#";;
esac
`)
	ayuda, err := HelpCLI("herramientafalsa")
	if err != nil || !strings.Contains(ayuda, "greet NAME") {
		t.Fatalf("ayuda = %q, %v", ayuda, err)
	}
	if !strings.Contains(Prompt("cli", "herramientafalsa", ayuda), "greet NAME") {
		t.Error("el prompt debe llevar la ayuda")
	}
	m, err := Parse(`{"name":"herramientafalsa","description":"d","tools":[{"name":"greet","description":"saluda","inputSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]},"exec":{"bin":"herramientafalsa","args":["greet","{name}"]}}]}`, "")
	if err != nil {
		t.Fatal(err)
	}
	if w, err := Verify(m, true); err != nil || len(w) > 0 {
		t.Fatalf("verify = %v %v", w, err)
	}
	salida, err := Invoke(m.Tools[0], map[string]any{"name": "a b; rm -rf / $(id)"})
	if err != nil {
		t.Fatal(err)
	}
	if salida != "a b; rm -rf / $(id)|2" {
		t.Errorf("el argumento debe llegar intacto y como un solo elemento: %q", salida)
	}
	if _, err := Invoke(m.Tools[0], map[string]any{}); err == nil {
		t.Error("falta un requerido: debe fallar")
	}
	if _, err := Invoke(m.Tools[0], map[string]any{"name": 3.0}); err == nil {
		t.Error("tipo equivocado: debe fallar")
	}
	m.Tools[0].Exec.Bin = "noexiste-loom-xyz"
	if _, err := Verify(m, true); err == nil {
		t.Error("un binario ausente no pasa la prueba de humo")
	}
}

func TestBloqueaInyeccionDeBanderas(t *testing.T) {
	fakeBin(t, "eco-banderas", `printf '%s ' "$@"`)
	props := map[string]any{
		"path":  map[string]any{"type": "string"},
		"out":   map[string]any{"type": "string"},
		"delta": map[string]any{"type": "string", "allow_leading_dash": true},
	}
	tool := Tool{Name: "t", InputSchema: map[string]any{"type": "object", "properties": props, "required": []any{"path", "out", "delta"}},
		Exec: &Exec{Bin: "eco-banderas", Args: []string{"{path}", "--out={out}", "{delta}"}}}
	for _, malo := range []string{"-rf", "--help", "--exec=sh"} {
		_, err := Invoke(tool, map[string]any{"path": malo, "out": "x", "delta": "1"})
		if err == nil || err.Error() != "argument value cannot start with '-' (flag injection attempt)" {
			t.Errorf("%q como valor posicional: err = %v", malo, err)
		}
	}
	salida, err := Invoke(tool, map[string]any{"path": "a", "out": "-raro", "delta": "-5"})
	if err != nil || salida != "a --out=-raro -5 " {
		t.Errorf("tras '=' o con allow_leading_dash el guion pasa: %q %v", salida, err)
	}
}

func TestOpenAPIAManifiestoYLlamada(t *testing.T) {
	var recibido map[string]any
	var ruta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ruta = r.Method + " " + r.URL.RequestURI()
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &recibido)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	specYAML := `openapi: 3.0.0
info: {title: Inventario Demo}
servers: [{url: "` + srv.URL + `/v1"}]
paths:
  /items/{item-id}:
    get:
      operationId: getItem
      parameters: [{name: item-id, in: path, required: true, schema: {type: string}}]
  /items:
    post:
      summary: create
      parameters: [{name: dry, in: query, required: false, schema: {type: boolean}}]
      requestBody:
        content:
          application/json:
            schema: {$ref: "#/components/schemas/Item"}
components:
  schemas:
    Item:
      type: object
      required: [name, qty]
      properties: {name: {type: string}, qty: {type: integer}, tags: {type: array}}
`
	f := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(f, []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _, err := FromOpenAPI(f, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "inventario_demo" || len(m.Tools) != 2 {
		t.Fatalf("manifiesto = %+v", m)
	}
	if _, err := Verify(m, false); err != nil {
		t.Fatalf("verify: %v", err)
	}
	var get, post Tool
	for _, tl := range m.Tools {
		if tl.Name == "getitem" {
			get = tl
		} else {
			post = tl
		}
	}
	if _, err := Invoke(get, map[string]any{"item_id": "a/b"}); err != nil || ruta != "GET /v1/items/a%2Fb" {
		t.Errorf("get: %v · %s", err, ruta)
	}
	if _, err := Invoke(post, map[string]any{"name": `x","admin":true,"y":"`, "qty": 3.0}); err != nil {
		t.Fatal(err)
	}
	if recibido["name"] != `x","admin":true,"y":"` || recibido["qty"] != 3.0 || recibido["admin"] != nil {
		t.Errorf("cuerpo recibido = %v · el valor no debe romper el JSON", recibido)
	}
}

func TestGuardarListarBorrar(t *testing.T) {
	root := t.TempDir()
	m := Manifest{Name: "uno", Tools: []Tool{{Name: "a"}}}
	if err := Save(root, m); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(Dir(root), "roto.json"), []byte("{"), 0o644)
	ok, malos := List(root)
	if len(ok) != 1 || len(malos) != 1 {
		t.Fatalf("list = %d buenos, %d malos", len(ok), len(malos))
	}
	if err := Remove(root, "uno"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(root, "../x"); err == nil {
		t.Error("nombre con ruta: debe negarse")
	}
}
