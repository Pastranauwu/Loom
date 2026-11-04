package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestSoltarSinRastro(t *testing.T) {
	out = ui.New(io.Discard, false)
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

// el esqueleto de una versión que se abandonó sin archivar: tiene carpeta
// en runs pero ni contrato ni la base de hoy. Su rama se descarta y su
// carpeta se queda; el reintento de un esqueleto de hoy se conserva.
func TestSoltarSinRastroRamaDeOtraBase(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	t.Setenv("LOOM_ROOMS", t.TempDir())
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		if salida, err := gitEn(root, args...); err != nil {
			t.Fatal(salida)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".loom", "config.yml"), []byte("base: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := room.Create(ctx, root, "T-001", "main"); err != nil {
		t.Fatal(err)
	}
	if salida, err := gitEn(root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "la base avanzó"); err != nil {
		t.Fatal(salida)
	}
	if _, err := room.Create(ctx, root, "T-002", "main"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"T-001", "T-002"} {
		if err := os.MkdirAll(filepath.Join(loop.RunsDir(root), id), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	soltarSinRastro(ctx, root, "T-001")
	soltarSinRastro(ctx, root, "T-002")
	if room.RamaExiste(ctx, root, "T-001") {
		t.Error("T-001 parte de una base vieja y no tiene contrato: se descarta")
	}
	if _, err := os.Stat(filepath.Join(loop.RunsDir(root), "T-001")); err != nil {
		t.Error("su carpeta de runs se conserva")
	}
	if !room.RamaExiste(ctx, root, "T-002") {
		t.Error("T-002 parte de la base de hoy: es el esqueleto que se retoma")
	}
}
