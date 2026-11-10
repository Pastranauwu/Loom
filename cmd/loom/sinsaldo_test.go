package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

// opencode sin saldo. La primera tarea choca con el
// 402 y queda pendiente, sin escalar de modelo; la que depende de ella
// no se lanza ni queda bloqueada, y `up` las retoma al recargar.
func TestSinSaldoDejaTodoPendienteSinEscalar(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "-q", "--allow-empty", "-m", "base"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	fixture, err := filepath.Abs("../../internal/executor/testdata/opencode-402.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	llamadas := filepath.Join(bin, "llamadas")
	// el fake cuenta solo las invocaciones del agente (`opencode run …`),
	// no las de catálogo (`opencode models --verbose`): lo que la prueba
	// mide es que un 402 no reintente ni escale.
	script := "#!/bin/sh\nif [ \"$1\" = run ]; then echo x >> '" + llamadas + "'; fi\ncat '" + fixture + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := config.Config{Base: "main", Modelos: map[string]string{"liviana": "barato", "media": "caro"}}
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	cfg, _ = config.Load(root)
	tareas := []task.Task{
		{Version: task.Version, ID: "T-001", Titulo: "base", ListoCuando: "test -f a.txt", TocarSolo: []string{"a.txt"}, Peso: "liviana", LimiteIntentos: 3},
		{Version: task.Version, ID: "T-002", Titulo: "encima", ListoCuando: "test -f b.txt", TocarSolo: []string{"b.txt"}, DependeDe: []string{"T-001"}, Peso: "liviana", LimiteIntentos: 3},
	}
	res := ejecutarOlas(context.Background(), root, cfg, executor.OpenCode{}, "", "", tareas, 1, nil, nil, nil)

	for _, r := range res {
		if r.Estado != estadoSinSaldo {
			t.Errorf("%s: estado %q (%s), quiero %q", r.ID, r.Estado, r.Motivo, estadoSinSaldo)
		}
		if s, _ := state.Get(root, r.ID); s.Estado != state.Pendiente {
			t.Errorf("%s: estado guardado %q, quiero pendiente", r.ID, s.Estado)
		}
	}
	if b, _ := os.ReadFile(llamadas); len(b) != 2 {
		t.Errorf("invocaciones de opencode = %d, quiero 1 (sin reintentos ni escalera)", len(b)/2)
	}
}

func TestRelevoEligeElMasBaratoDeOtroProveedor(t *testing.T) {
	t.Cleanup(func() { agotados.Range(func(k, _ any) bool { agotados.Delete(k); return true }) })
	cfg := config.Config{Modelos: map[string]string{"liviana": "google/gemini-flash", "media": "claude-haiku-5-5", "pesada": "claude-sonnet-5-5"}}
	oc := executor.OpenCode{}
	if p := proveedorDe(oc, "google/gemini-flash"); p != "google" {
		t.Fatalf("proveedor = %q", p)
	}
	agotados.Store("google", true)
	if m := relevo(cfg, oc); m != "claude-haiku-5-5" {
		t.Errorf("relevo = %q, quiero el medio de claude", m)
	}
	agotados.Store("claude", true)
	if m := relevo(cfg, oc); m != "" {
		t.Errorf("sin proveedores con saldo y dio %q", m)
	}
}
