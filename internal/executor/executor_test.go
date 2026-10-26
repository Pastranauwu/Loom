package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBin instala un CLI falso en PATH y devuelve su directorio.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("los binarios falsos son scripts de shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func reqDePrueba() Request {
	return Request{
		RoomPath: ".",
		Prompt:   "haz la tarea",
		Timeout:  5 * time.Second,
		Env:      []string{"PORT=4321"},
	}
}

func TestOpenCodeRun(t *testing.T) {
	fakeBin(t, "opencode", `cat <<'EOF'
{"type":"step_start"}
{"type":"tool_use","part":{"path":"src/export/csv.go"}}
{"type":"tool_use","part":{"path":"src/export/csv.go"}}
{"type":"step_finish","tokens":{"input":100,"output":40}}
{"type":"step_finish","tokens":{"input":50,"output":10}}
EOF`)
	e := OpenCode{}
	if err := e.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	res, err := e.Run(context.Background(), reqDePrueba())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d", res.ExitCode)
	}
	if len(res.FilesChanged) != 1 || res.FilesChanged[0] != "src/export/csv.go" {
		t.Errorf("FilesChanged = %v", res.FilesChanged)
	}
	if res.Tokens.Input != 150 || res.Tokens.Output != 50 {
		t.Errorf("Tokens = %+v", res.Tokens)
	}
}

func TestClaudeRun(t *testing.T) {
	fakeBin(t, "claude", `echo '{"type":"result","result":"hecho","usage":{"input_tokens":10,"output_tokens":20}}'`)
	e := Claude{}
	if err := e.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	res, err := e.Run(context.Background(), reqDePrueba())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Tokens.Input != 10 || res.Tokens.Output != 20 {
		t.Errorf("Tokens = %+v", res.Tokens)
	}
}

func TestRunExitCode(t *testing.T) {
	fakeBin(t, "opencode", "exit 3")
	res, err := OpenCode{}.Run(context.Background(), reqDePrueba())
	if err == nil {
		t.Fatal("Run debió devolver error")
	}
	if res.ExitCode != 3 {
		t.Errorf("ExitCode = %d, quiero 3", res.ExitCode)
	}
}

func TestRunTimeout(t *testing.T) {
	fakeBin(t, "opencode", "sleep 10")
	req := reqDePrueba()
	req.Timeout = 100 * time.Millisecond
	res, err := OpenCode{}.Run(context.Background(), req)
	if err == nil {
		t.Fatal("Run con timeout debió devolver error")
	}
	if res.ExitCode != 124 {
		t.Errorf("ExitCode = %d, quiero 124", res.ExitCode)
	}
}

func TestRunPasaEnv(t *testing.T) {
	fakeBin(t, "opencode", "echo \"port=$PORT\"")
	res, err := OpenCode{}.Run(context.Background(), reqDePrueba())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Stdout, "port=4321") {
		t.Errorf("el entorno del cuarto no llegó al agente: %q", res.Stdout)
	}
}

func TestAvailableSinBinario(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := OpenCode{}.Available()
	if err == nil || !strings.Contains(err.Error(), "opencode") {
		t.Errorf("Available = %v", err)
	}
	err = Claude{}.Available()
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Errorf("Available = %v", err)
	}
}

func TestParseOpenCodeEventsIgnoraBasura(t *testing.T) {
	stdout := "no json\n{\"mal\":true}\n{\"type\":\"x\",\"path\":\"a/b.go\"}\n"
	files, _, _ := parseOpenCodeEvents(stdout)
	if fmt.Sprint(files) != "[a/b.go]" {
		t.Errorf("files = %v", files)
	}
}

func TestOpenCodeExtraeTexto(t *testing.T) {
	stdout := `{"type":"message","part":{"type":"text","text":"hola"}}
{"type":"message","part":{"type":"text","text":"mundo"}}`
	_, text, _ := parseOpenCodeEvents(stdout)
	if text != "hola\nmundo" {
		t.Errorf("text = %q, quiero hola\\nmundo", text)
	}
}

func TestClaudeExtraeTexto(t *testing.T) {
	stdout := `{"type":"result","result":"[{\"titulo\":\"x\"}]","usage":{"input_tokens":1,"output_tokens":2}}`
	if got, _ := parseClaudeStream(stdout); got != `[{"titulo":"x"}]` {
		t.Errorf("text = %q", got)
	}
}

// opencode anida el gasto bajo "part": leerlo solo en la raíz daba
// siempre 0 tokens.
func TestParseOpenCodeTokensAnidados(t *testing.T) {
	stream := `{"type":"step_finish","part":{"type":"step-finish","tokens":{"total":21897,"input":34,"output":67}}}
{"type":"step_finish","part":{"type":"step-finish","tokens":{"input":6,"output":4}}}
{"type":"text","part":{"type":"text","text":"listo"}}`
	_, texto, usage := parseOpenCodeEvents(stream)
	if usage.Input != 40 || usage.Output != 71 {
		t.Errorf("usage = %+v, quiero {Input:40 Output:71}", usage)
	}
	if texto != "listo" {
		t.Errorf("texto = %q", texto)
	}
}

func TestClaudePasaEsfuerzoExplicito(t *testing.T) {
	fakeBin(t, "claude", `printf '%s\n' "$@"`)
	req := reqDePrueba()
	req.Effort = "medium"
	res, err := (Claude{}).Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Stdout, "--effort\nmedium\n") {
		t.Fatalf("argumentos: %q", res.Stdout)
	}
	req.Effort = ""
	res, err = (Claude{}).Run(context.Background(), req)
	if err != nil || strings.Contains(res.Stdout, "--effort") {
		t.Fatalf("alteró el esfuerzo por defecto: %q, %v", res.Stdout, err)
	}
}

// Con claude casi todo el prompt llega como caché: input_tokens solo
// cuenta lo que quedó fuera de ella. Y el total real está en modelUsage:
// el usage del resultado se queda corto (789k contra 4,58M medidos).
func TestUsoConCache(t *testing.T) {
	stream := `{"type":"system","subtype":"init","tools":["Bash"]}
{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg_1","usage":{"input_tokens":10,"cache_creation_input_tokens":11106,"cache_read_input_tokens":12306,"output_tokens":1}}}
{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg_1","usage":{"input_tokens":10,"cache_creation_input_tokens":11106,"cache_read_input_tokens":12306,"output_tokens":1}}}
{"type":"user","parent_tool_use_id":null,"message":{"role":"user"}}
{"type":"assistant","parent_tool_use_id":"toolu_9","message":{"id":"msg_sub","usage":{"input_tokens":3,"cache_creation_input_tokens":900,"cache_read_input_tokens":0,"output_tokens":5}}}
{"type":"assistant","parent_tool_use_id":null,"message":{"id":"msg_2","usage":{"input_tokens":2,"cache_creation_input_tokens":300,"cache_read_input_tokens":23412,"output_tokens":40}}}
{"type":"result","result":"listo","num_turns":2,"total_cost_usd":0.7855,"usage":{"input_tokens":129,"cache_creation_input_tokens":42631,"cache_read_input_tokens":789576,"output_tokens":11848},"modelUsage":{"claude-haiku-4-5":{"inputTokens":554,"outputTokens":28526,"cacheReadInputTokens":4579893,"cacheCreationInputTokens":121937}}}`
	texto, u := parseClaudeStream(stream)
	quiero := Usage{Input: 554, Output: 28526, CacheRead: 4579893, CacheWrite: 121937, Turns: 2, FirstTurn: 23422, FirstTurnWrite: 11106, CostUSD: 0.7855}
	if texto != "listo" || u != quiero {
		t.Errorf("claude: %q %+v\nquiero %+v", texto, u, quiero)
	}
	// la salida de --output-format json (un solo resultado, sin turnos) sigue leyéndose
	if _, viejo := parseClaudeStream(`{"type":"result","num_turns":19,"usage":{"input_tokens":129,"output_tokens":11848}}`); viejo != (Usage{Input: 129, Output: 11848, Turns: 19}) {
		t.Errorf("json: %+v", viejo)
	}

	_, _, oc := parseOpenCodeEvents(`{"type":"step_finish","part":{"cost":0.5,"tokens":{"input":34,"output":67,"cache":{"read":900,"write":50}}}}` + "\n" +
		`{"type":"step_finish","part":{"cost":0.25,"tokens":{"input":6,"output":3,"cache":{"read":100,"write":0}}}}`)
	if oc != (Usage{Input: 40, Output: 70, CacheRead: 1000, CacheWrite: 50, Turns: 2, FirstTurn: 984, FirstTurnWrite: 50, CostUSD: 0.75}) {
		t.Errorf("opencode: %+v", oc)
	}
}

// Un 429 no llega al bucle: la invocación espera al reset que reporta la
// respuesta y relanza. Los fixtures son líneas copiadas tal cual de la
// corrida A del benchmark (prueba-bench, 23 sep 2026): claude-429.jsonl
// de T-011/intento-2.log, que chocó con la cuota de 5h, y claude-ok.jsonl
// de T-006/intento-2.log.
func TestClaudeEsperaElResetDeCuota(t *testing.T) {
	cuota, _ := filepath.Abs("testdata/claude-429.jsonl")
	ok, _ := filepath.Abs("testdata/claude-ok.jsonl")
	marca := filepath.Join(t.TempDir(), "ya")
	fakeBin(t, "claude", fmt.Sprintf("if [ ! -f %q ]; then touch %q; cat %q; exit 1; fi\ncat %q\n", marca, marca, cuota, ok))
	var esperas []time.Duration
	dormirOriginal := dormir
	dormir = func(_ context.Context, d time.Duration) error { esperas = append(esperas, d); return nil }
	t.Cleanup(func() { dormir = dormirOriginal })

	res, err := (Claude{}).Run(context.Background(), reqDePrueba())
	if err != nil || res.ExitCode != 0 || !strings.HasPrefix(res.Text, "Listo.") {
		t.Fatalf("no relanzó tras el 429: %v %d %q", err, res.ExitCode, res.Text)
	}
	reset := time.Unix(1790203200, 0).Add(margenReset) // resetsAt del rate_limit_event rechazado
	if len(esperas) != 1 || time.Now().Add(esperas[0]).Sub(reset).Abs() > time.Second {
		t.Fatalf("no esperó al reset de la respuesta: %v", esperas)
	}
}

// Toda corrida sana emite un rate_limit_event con status allowed: tomarlo
// por cuota agotada dormiría cada invocación. Líneas reales de
// T-006/intento-2.log de la corrida A.
func TestCuotaNoAgotadaEnCorridaSana(t *testing.T) {
	b, err := os.ReadFile("testdata/claude-ok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, agotada := cuotaAgotada(string(b)); agotada {
		t.Error("una corrida sana no es cuota agotada")
	}
}

// Los nombres de agente se escriben dos veces a mano, en el mapa y en el
// JSON: un JSON roto o un --agent que no está en la config deja a
// opencode sin arrancar.
func TestOpenCodeConfigDefineCadaAgente(t *testing.T) {
	var cfg struct {
		Agent map[string]any `json:"agent"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(entornoOpenCode[0], "OPENCODE_CONFIG_CONTENT=")), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT no es JSON: %v", err)
	}
	for rol, agente := range agenteOpenCode {
		if cfg.Agent[agente] == nil {
			t.Errorf("rol %q: el agente %q no está en la config", rol, agente)
		}
	}
}

func TestOpenCodeRunReportaErrorDelProveedor(t *testing.T) {
	fixture, err := filepath.Abs("testdata/opencode-402.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin(t, "opencode", "cat '"+fixture+"'; exit 1")
	res, err := OpenCode{}.Run(context.Background(), reqDePrueba())
	want := "402 Upstream request failed: Insufficient account funds"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("err = %v, quiero %q", err, want)
	}
	if !strings.Contains(res.Stderr, want) {
		t.Errorf("Stderr = %q", res.Stderr)
	}
}

// stream real del planificador: lo que reporta tiene que dejar ver qué
// corrió, qué leyó y cuánto escribió cada turno
func TestOpenCodeRunReportaAvances(t *testing.T) {
	fixture, err := filepath.Abs("testdata/opencode-planificador.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	cuarto := t.TempDir()
	fakeBin(t, "opencode", "sed 's#/cuarto#"+cuarto+"#g' '"+fixture+"'")
	req := reqDePrueba()
	req.RoomPath = cuarto
	var avances []string
	req.Avance = func(s string) { avances = append(avances, s) }
	if _, err := (OpenCode{}).Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(avances, "\n")
	for _, want := range []string{"$ ls -la", "lee loom.spec.yml", "turno 1 ·", "respuesta escrita · 15 caracteres"} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en avances:\n%s", want, got)
		}
	}
}

func TestCodexRunLeeElStreamReal(t *testing.T) {
	fixture, err := filepath.Abs("testdata/codex-ok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	fakeBin(t, "codex", `echo "$@" > "$PWD/args.txt"; cat `+fixture)
	dir := t.TempDir()
	req := reqDePrueba()
	req.RoomPath, req.Model, req.Effort, req.Rol = dir, "gpt-6-luna", "low", RolTexto
	var avances []string
	req.Avance = func(s string) { avances = append(avances, s) }

	res, err := Codex{}.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "hola" {
		t.Errorf("Text = %q, quiero el último mensaje del agente", res.Text)
	}
	// input_tokens (23499) incluye los 17920 de caché: no se cuentan dos veces
	if u := res.Tokens; u.Input != 5579 || u.CacheRead != 17920 || u.Output != 111 || u.Turns != 1 || u.FirstTurn != 23499 {
		t.Errorf("Tokens = %+v", u)
	}
	if j := strings.Join(avances, "\n"); !strings.Contains(j, "$ cat a.txt") || !strings.Contains(j, "turno 1") {
		t.Errorf("avances = %q", j)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, quiero := range []string{"exec --json", "--sandbox read-only", "--model gpt-6-luna", `model_reasoning_effort="low"`, "--ignore-user-config"} {
		if !strings.Contains(string(args), quiero) {
			t.Errorf("falta %q en los argumentos: %s", quiero, args)
		}
	}
	if strings.Contains(string(args), "bypass") {
		t.Error("un rol que no escribe no puede correr sin sandbox")
	}
}

func TestCodexReportaElErrorYLosArchivos(t *testing.T) {
	files, _, _, fallo := parseCodexEvents(`{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"src/a.go","kind":"update"},{"path":"src/a.go","kind":"update"}]}}
{"type":"turn.failed","error":{"message":"You've hit your usage limit"}}`)
	if len(files) != 1 || files[0] != "src/a.go" || !strings.Contains(fallo, "usage limit") {
		t.Errorf("files = %v fallo = %q", files, fallo)
	}
}

func TestCodexModelsSoloLosVisibles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if _, err := (Codex{}).Models(context.Background()); err == nil {
		t.Error("sin caché de modelos debe dar error, no un catálogo vacío")
	}
	cache := `{"models":[{"slug":"gpt-6-sol","visibility":"list"},{"slug":"codex-auto-review","visibility":"hide"}]}`
	if err := os.WriteFile(filepath.Join(home, "models_cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := Codex{}.Models(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != "gpt-6-sol" {
		t.Errorf("ids = %v err = %v", ids, err)
	}
}
