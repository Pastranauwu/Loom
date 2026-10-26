package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func TestSoltarSinRastro(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOOM_ROOMS", t.TempDir())
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if salida, err := gitEn(root, args...); err != nil {
			t.Fatal(salida)
		}
	}
	ctx := context.Background()
	for _, id := range []string{"T-001", "T-002"} {
		if _, err := room.Create(ctx, root, id, "main"); err != nil {
			t.Fatal(err)
		}
	}
	// T-002 corrió en esta instalación: tiene su carpeta en runs
	if err := os.MkdirAll(filepath.Join(loop.RunsDir(root), "T-002"), 0o755); err != nil {
		t.Fatal(err)
	}
	soltarSinRastro(ctx, root, "T-001")
	soltarSinRastro(ctx, root, "T-002")
	if room.RamaExiste(ctx, root, "T-001") {
		t.Error("T-001 no dejó rastro en runs: su rama es de otra corrida y debía descartarse")
	}
	if !room.RamaExiste(ctx, root, "T-002") {
		t.Error("T-002 tiene rastro: su cuarto se conserva")
	}
}

func TestVerdesPerdidas(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LOOM_ROOMS", t.TempDir())
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if salida, err := gitEn(root, args...); err != nil {
			t.Fatal(salida)
		}
	}
	ctx := context.Background()
	// T-001 lista con su rama; T-002 lista y sin rama ni entrega
	if _, err := room.Create(ctx, root, "T-001", "main"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-001", "T-002"} {
		if err := state.Save(root, state.State{ID: id, Estado: state.Lista}); err != nil {
			t.Fatal(err)
		}
	}
	got := verdesPerdidas(ctx, root, "main", []task.Task{{ID: "T-003", DependeDe: []string{"T-001", "T-002"}}})
	if len(got) != 1 || got[0] != "T-002" {
		t.Errorf("verdesPerdidas = %v, quiero [T-002]", got)
	}
}
