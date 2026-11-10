package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// claudeFalso deja en PATH un `claude` que imprime sus argumentos como
// resultado, con el subtipo dado.
func claudeFalso(t *testing.T, subtipo string, salida int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script de shell")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '{\"subtype\":\"" + subtipo + "\",\"session_id\":\"s-123\",\"num_turns\":1,\"result\":\"%s\",\"type\":\"result\"}\\n' \"$*\"\nexit " + string(rune('0'+salida)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestClaudeContinuaLaSesionYDevuelveLaSuya(t *testing.T) {
	claudeFalso(t, "success", 0)
	res, err := Claude{}.Run(context.Background(), Request{RoomPath: t.TempDir(), Prompt: "corrige", Sesion: "s-000", TopeUSD: 5, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sesion != "s-123" {
		t.Errorf("Sesion = %q, quiero la del evento result", res.Sesion)
	}
	for _, quiere := range []string{"--resume s-000", "--max-budget-usd 5"} {
		if !strings.Contains(res.Text, quiere) {
			t.Errorf("faltó %q en los argumentos · %s", quiere, res.Text)
		}
	}
}

func TestClaudeSinSesionNiTopeNoPasaLasBanderas(t *testing.T) {
	claudeFalso(t, "success", 0)
	res, err := Claude{}.Run(context.Background(), Request{RoomPath: t.TempDir(), Prompt: "hola", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Text, "--resume") || strings.Contains(res.Text, "--max-budget-usd") {
		t.Errorf("banderas de más · %s", res.Text)
	}
}

func TestClaudeCortadoPorTopeEsErrTope(t *testing.T) {
	claudeFalso(t, "error_max_budget_usd", 1)
	_, err := Claude{}.Run(context.Background(), Request{RoomPath: t.TempDir(), Prompt: "hola", TopeUSD: 2, Timeout: 10 * time.Second})
	if !errors.Is(err, ErrTope) {
		t.Errorf("err = %v, quiero ErrTope", err)
	}
}

func TestOpenCodeContinuaLaSesionYMideElUltimoTurno(t *testing.T) {
	fakeBin(t, "opencode", `echo "$*" >&2
echo '{"type":"step_finish","sessionID":"ses_abc123","part":{"type":"step-finish","tokens":{"input":100,"output":5,"cache":{"read":900,"write":0}}}}'
echo '{"type":"step_finish","sessionID":"ses_abc123","part":{"type":"step-finish","tokens":{"input":50,"output":5,"cache":{"read":1950,"write":0}}}}'
`)
	res, err := OpenCode{}.Run(context.Background(), Request{RoomPath: t.TempDir(), Prompt: "corrige", Sesion: "ses_previa", Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sesion != "ses_abc123" || res.Tokens.LastTurn != 2000 {
		t.Errorf("Sesion = %q, LastTurn = %d", res.Sesion, res.Tokens.LastTurn)
	}
	if !strings.Contains(res.Stderr, "--session ses_previa") {
		t.Errorf("no pasó --session · %s", res.Stderr)
	}
}
