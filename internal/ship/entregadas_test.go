package ship

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ship --todas volvía a meter las 29 tareas de la entrega
// anterior, ya integrada a main, y chocaban con lo que vino después
func TestEntregadasPorTrailerYPorRegistro(t *testing.T) {
	root := repoConCommit(t)
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	// una entrega con trailer (squash o rebase conservan el mensaje)
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "feat: a", "-m", "Agent: haiku\nTarea: T-001")
	// una entrega vieja sin trailer, integrada con merge: la cubre el registro
	gitCmd(t, root, "checkout", "-q", "-b", "loom/_entrega")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "feat: b")
	if err := RegistrarEntrega(root, "loom/_entrega", []string{"T-002", "T-003"}); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "checkout", "-q", "main")

	got := Entregadas(context.Background(), root, "main")
	if !got["T-001"] || got["T-002"] {
		t.Fatalf("antes del merge: %v", got)
	}
	gitCmd(t, root, "merge", "-q", "--ff-only", "loom/_entrega")
	got = Entregadas(context.Background(), root, "main")
	if !got["T-001"] || !got["T-002"] || !got["T-003"] {
		t.Fatalf("después del merge: %v", got)
	}
}
