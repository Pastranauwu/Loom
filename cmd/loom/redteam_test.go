package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/redteam"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func repoDeRedteam(t *testing.T) (string, task.Task) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.MkdirAll(config.TasksDir(root), 0o755)
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644)
	os.WriteFile(filepath.Join(root, "pkg", "pkg.go"), []byte("package pkg\n\nfunc Boom(p *int) int {\n\treturn *p\n}\n"), 0o644)
	if err := (config.Config{Base: "main", Redteam: "block"}).Save(root); err != nil {
		t.Fatal(err)
	}
	for _, a := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	tk := task.Task{Version: task.Version, ID: "T-001", Titulo: "boom", ListoCuando: "go test ./pkg", TocarSolo: []string{"pkg/**"}, Expone: []string{"pkg.Boom(p *int) int"}, LimiteIntentos: 3}
	if err := task.Save(config.TasksDir(root), tk); err != nil {
		t.Fatal(err)
	}
	return root, tk
}

type atacanteFijo struct{ respuesta string }

func (a atacanteFijo) Generar(context.Context, string) (string, error) { return a.respuesta, nil }

const ataqueNil = `{"attacks":[{"name":"nil","invariant":"nil_edge","target":"pkg.Boom(p *int) int","test":"func TestRedTeam1(t *testing.T) { Boom(nil) }"}]}`

func TestPasoRedteamBloqueaSoloConCrashConfirmadoEnModoBlock(t *testing.T) {
	out = ui.New(io.Discard, false)
	root, tk := repoDeRedteam(t)
	cfg, _ := config.Load(root)
	atacar := func(modo string) (string, bool) {
		inf, err := atacarTarea(context.Background(), root, cfg, tk, atacanteFijo{ataqueNil}, false)
		if err != nil {
			t.Fatal(err)
		}
		conf := inf.Confirmados()
		if len(conf) != 1 {
			t.Fatalf("debía confirmar el panic: %+v", inf)
		}
		return conf[0].Detalle, modo != "block"
	}
	if det, ok := atacar("block"); ok || !strings.Contains(det, "pkg.go") {
		t.Errorf("block frena con el crash: %q ok=%v", det, ok)
	}
	if _, ok := atacar("warn"); !ok {
		t.Error("warn solo avisa")
	}
}

func TestRedteamReopenReabreLaTareaConElTestReproductor(t *testing.T) {
	out = ui.New(io.Discard, false)
	root, tk := repoDeRedteam(t)
	if err := state.Save(root, state.State{ID: "T-001", Estado: state.Lista}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(root)
	inf, err := atacarTarea(context.Background(), root, cfg, tk, atacanteFijo{ataqueNil}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(inf.Confirmados()) != 1 {
		t.Fatalf("informe: %+v", inf)
	}
	if es, _ := os.ReadDir(filepath.Join(config.Dir(root), "redteam", "T-001")); len(es) != 1 {
		t.Errorf("--keep-tests/--reopen guardan el test: %d", len(es))
	}
	n, err := reabrirContratos(root, []task.Task{tk}, map[string]redteam.Informe{"T-001": inf})
	if err != nil || n != 1 {
		t.Fatalf("reabiertas=%d err=%v", n, err)
	}
	t1, err := task.Load(config.TasksDir(root), "T-001")
	if err != nil || !strings.Contains(t1.ListoCuando, "go test ./pkg -run '^TestRedTeam1$'") || !strings.Contains(t1.Notas, "RED TEAM") || len(t1.TocarSolo) != 2 {
		t.Errorf("contrato reabierto: %v %+v", err, t1)
	}
	if st, _ := state.Get(root, "T-001"); st.Estado != state.Pendiente {
		t.Errorf("estado = %s, quiero pendiente", st.Estado)
	}
}
