package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/mcp"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestBoardDeltaSoloCambios(t *testing.T) {
	dir := t.TempDir()
	uno := `{"pendiente":[{"id":"T-001","titulo":"a"},{"id":"T-002","titulo":"b"}]}`
	if out, _ := boardDelta(dir, uno, false); !strings.Contains(out, "T-001") || !strings.Contains(out, "T-002") {
		t.Fatalf("la primera llamada lista todo: %q", out)
	}
	if out, _ := boardDelta(dir, uno, false); !strings.HasPrefix(out, "no changes") {
		t.Fatalf("sin cambios debe decirlo corto: %q", out)
	}
	dos := `{"listo":[{"id":"T-001","titulo":"a"}],"pendiente":[{"id":"T-002","titulo":"b"}]}`
	out, _ := boardDelta(dir, dos, false)
	if !strings.Contains(out, "T-001 pendiente→listo") || strings.Contains(out, "T-002") {
		t.Fatalf("solo debe salir lo que cambió: %q", out)
	}
}

// exeFalso hace de binario de loom: devuelve el argv que recibió.
func exeFalso(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("binario falso en shell")
	}
	exe := filepath.Join(t.TempDir(), "loom")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho \"argv: $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestMCPExponeElCicloCompleto(t *testing.T) {
	nombres := map[string]bool{}
	for _, tl := range mcpTools("loom") {
		nombres[tl.Name] = true
		if tl.Schema["type"] != "object" {
			t.Errorf("%s sin schema object", tl.Name)
		}
	}
	for _, n := range []string{"loom_wrap", "loom_wrap_list", "loom_wrap_show", "loom_wrap_rm", "loom_redteam", "loom_dna", "loom_heal", "loom_map"} {
		if !nombres[n] {
			t.Errorf("falta la herramienta %s", n)
		}
	}
}

func TestMCPWrapYRedteamPorJSONRPC(t *testing.T) {
	exe := exeFalso(t)
	dir := t.TempDir()
	in := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"loom_wrap","arguments":{"dir":"` + dir + `","curl":"curl -H 'X: 1' https://h/x","name":"api"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"loom_redteam","arguments":{"dir":"` + dir + `","id":"T-001","reopen":true}}}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"loom_dna","arguments":{"dir":"` + dir + `","action":"add","name":"zero-deps"}}}
{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"loom_redteam","arguments":{"dir":"` + dir + `"}}}
`
	var out bytes.Buffer
	if err := mcp.Serve(strings.NewReader(in), &out, "loom", "t", mcpTools(exe)); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"argv: wrap --plain --curl curl -H 'X: 1' https://h/x --name api",
		"argv: redteam T-001 --reopen --plain",
		"argv: dna add zero-deps --plain",
		"pass id or all",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en:\n%s", want, got)
		}
	}
}

func TestBridgeToolsLeeElRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom", "bridges"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := `{"name":"api","tools":[{"name":"ping","inputSchema":{"type":"object","properties":{}},"http":{"method":"GET","url":"https://h/ping"}}]}`
	if err := os.WriteFile(filepath.Join(root, ".loom", "bridges", "api.json"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	prev, _ := os.Getwd()
	defer os.Chdir(prev)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	tools := bridgeTools()
	if len(tools) != 1 || tools[0].Name != "bridge_api_ping" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestTaskSetSchemaTipado(t *testing.T) {
	for _, tl := range mcpTools("loom") {
		if tl.Name != "loom_task_set" {
			continue
		}
		props := tl.Schema["properties"].(map[string]any)
		peso := props["weight"].(map[string]any)
		if peso["type"] != "string" || strings.Join(peso["enum"].([]string), ",") != "light,medium,heavy" {
			t.Errorf("weight = %v", peso)
		}
		if props["max_retries"].(map[string]any)["type"] != "integer" || props["exposes"].(map[string]any)["type"] != "array" || props["network"].(map[string]any)["type"] != "boolean" {
			t.Errorf("props = %v", props)
		}
		return
	}
	t.Fatal("falta loom_task_set")
}

func TestTaskSetPersisteElContratoYSeLee(t *testing.T) {
	root, _ := repoDeRedteam(t)
	set := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"loom_task_set","arguments":{"dir":"` + root + `","id":"T-001","exposes":["pkg.Boom(p *int) int","pkg.Safe(p *int) (int, error)"],"weight":"heavy","max_retries":5,"network":true}}}` + "\n"
	var resp bytes.Buffer
	if err := mcp.Serve(strings.NewReader(set), &resp, "loom", "t", mcpTools("loom")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(resp.String(), `"isError":true`) {
		t.Fatalf("set falló: %s", resp.String())
	}
	crudo, _ := os.ReadFile(filepath.Join(config.TasksDir(root), "T-001.md"))
	for _, quiero := range []string{"exposes: [", "pkg.Safe(p *int) (int, error)", "weight: heavy", "max_retries: 5", "network: true"} {
		if !strings.Contains(string(crudo), quiero) {
			t.Errorf("el contrato en disco no trae %q:\n%s", quiero, crudo)
		}
	}
	tk, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil || len(tk.Expone) != 2 || tk.LimiteIntentos != 5 || !tk.Red || task.PesoEn(tk.Peso) != "heavy" {
		t.Fatalf("releído = %+v, %v", tk, err)
	}

	get := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"loom_task_show","arguments":{"dir":"` + root + `","id":"T-001"}}}` + "\n"
	resp.Reset()
	_ = mcp.Serve(strings.NewReader(get), &resp, "loom", "t", mcpTools("loom"))
	for _, quiero := range []string{`\"weight\": \"heavy\"`, `\"max_retries\": 5`, `\"network\": true`, `pkg.Safe`} {
		if !strings.Contains(resp.String(), quiero) {
			t.Errorf("loom_task_show no trae %s:\n%s", quiero, resp.String())
		}
	}

	var tablero bytes.Buffer
	prevOut, prevDir := out, mustGetwd()
	out = ui.New(&tablero, true)
	defer func() { out = prevOut; os.Chdir(prevDir) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := runBoard(); err != nil {
		t.Fatal(err)
	}
	for _, quiero := range []string{`"weight": "heavy"`, `"max_retries": 5`, `"exposes": [`} {
		if !strings.Contains(tablero.String(), quiero) {
			t.Errorf("board --json no trae %s:\n%s", quiero, tablero.String())
		}
	}
}

func TestManualEnMCP(t *testing.T) {
	in := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"loom_manual","arguments":{}}}` + "\n"
	mcp.Instructions = instruccionesMCP
	defer func() { mcp.Instructions = "" }()
	var out bytes.Buffer
	if err := mcp.Serve(strings.NewReader(in), &out, "loom", "t", mcpTools("loom")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"instructions"`) || !strings.Contains(out.String(), "loom_wrap") {
		t.Fatalf("initialize sin instructions o manual vacío: %s", out.String())
	}
	// cada herramienta que existe aparece en el manual: uno nuevo sin documentar falla aquí
	for _, tl := range mcpTools("loom") {
		if !strings.Contains(manualAgentes, "`"+tl.Name+"`") {
			t.Errorf("%s no está en manual.md", tl.Name)
		}
	}
}

func TestBoardWaitBloqueaHastaQueAlgoCambie(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("binario falso en shell")
	}
	dir := t.TempDir()
	// el tablero cambia a la tercera consulta
	exe := filepath.Join(t.TempDir(), "loom")
	script := `#!/bin/sh
n=$(cat cuenta 2>/dev/null || echo 0); n=$((n+1)); echo $n > cuenta
if [ $n -ge 3 ]; then echo '{"lista":[{"id":"T-001","titulo":"a"}]}'; else echo '{"en_curso":[{"id":"T-001","titulo":"a"}]}'; fi
`
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	viejo := esperaBoard
	esperaBoard = 10 * time.Millisecond
	defer func() { esperaBoard = viejo }()
	var board mcp.Tool
	for _, tl := range mcpTools(exe) {
		if tl.Name == "loom_board" {
			board = tl
		}
	}
	if _, err := board.Call(map[string]any{"dir": dir}); err != nil {
		t.Fatal(err)
	}
	out, err := board.Call(map[string]any{"dir": dir, "wait": float64(5)})
	if err != nil || !strings.Contains(out, "en_curso→lista") {
		t.Fatalf("wait debe volver con el cambio, no con 'no changes': %q %v", out, err)
	}
	if out, _ := board.Call(map[string]any{"dir": dir}); !strings.HasPrefix(out, "no changes") {
		t.Fatalf("sin wait responde al momento: %q", out)
	}
}

func TestTerminoVueloAvisaUnaVez(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "x.log")
	if err := os.WriteFile(log, []byte("planeando\n✓ 4 tareas\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := &vuelo{fin: make(chan struct{}), log: log}
	vuelos[dir] = v
	if terminoVuelo(dir) != "" {
		t.Fatal("sigue corriendo: no hay aviso")
	}
	close(v.fin)
	if got := terminoVuelo(dir); !strings.Contains(got, "ended (ok)") || !strings.Contains(got, "4 tareas") {
		t.Fatalf("aviso = %q", got)
	}
	if terminoVuelo(dir) != "" {
		t.Error("el aviso sale una sola vez")
	}
}
