package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Pastranauwu/loom/internal/room"
)

func TestLiberarCuartosQuitaLosEntregadosYDejaLosDemas(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "base")
	ctx := context.Background()
	for _, id := range []string{"T-001", "T-002", "_integra", "_entrega"} {
		git("worktree", "add", "-q", filepath.Join(room.Dir(root), id), "-b", room.Branch(id), "main")
	}
	existe := func(id string) bool {
		_, err := os.Stat(filepath.Join(room.Dir(root), id))
		return err == nil
	}

	// T-002 sigue en vuelo: su cuarto y los de integración no se tocan
	if n := liberarCuartos(ctx, root, []string{"T-001"}); n != 1 || existe("T-001") || !existe("T-002") || !existe("_integra") {
		t.Fatalf("liberados=%d T-001=%v T-002=%v _integra=%v", n, existe("T-001"), existe("T-002"), existe("_integra"))
	}
	if room.RamaExiste(ctx, root, "T-001") {
		t.Error("la rama de una tarea entregada se borra: así la reconoce run")
	}

	if n := liberarCuartos(ctx, root, []string{"T-002"}); n != 3 || existe("_integra") || existe("_entrega") {
		t.Fatalf("sin tareas en vuelo se van también integración y entrega: liberados=%d", n)
	}
	if !room.RamaExiste(ctx, root, "_entrega") {
		t.Error("la rama de entrega es el PR: se quita la carpeta, no la rama")
	}
}
