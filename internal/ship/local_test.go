package ship

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

func entregaLocal(t *testing.T, integrar bool) (string, Entrega) {
	t.Helper()
	return entregaLocalEn(t, repoConCommit(t), config.Config{Base: "main", Pruebas: "true"}, integrar)
}

func entregaLocalEn(t *testing.T, root string, cfg config.Config, integrar bool) (string, Entrega) {
	t.Helper()
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")
	cuartoDeTarea(t, root, "T-002", "b.go")
	base := strings.TrimSpace(gitCmd(t, root, "rev-parse", "main"))

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:     root,
		Config:   cfg,
		Base:     "main",
		Tareas:   []task.Task{tareaEntrega("T-001", "a.go"), tareaEntrega("T-002", "b.go", "T-001")},
		Commits:  map[string]string{"T-001": base, "T-002": base},
		Integrar: integrar,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })
	if !e.Aprobado {
		t.Fatalf("entrega local no aprobada · %s · pasos %+v", e.PrimerMotivo(), e.Pasos)
	}
	return root, e
}

// Sin remoto, la entrega no muere pidiendo origin: el PR queda en el repo,
// la rama para revisar y mergear, y la descripción en .loom/pr/.
func TestEntregarTodasSinRemotoDejaPRLocal(t *testing.T) {
	root, e := entregaLocal(t, false)

	if !strings.HasPrefix(e.PR, PRLocal) {
		t.Errorf("PR = %q · debe ser local", e.PR)
	}
	if e.Integrado {
		t.Error("sin --integrar la base no se toca")
	}
	if log := gitCmd(t, root, "log", "--format=%s", "main.."+RamaEntrega); len(strings.Split(strings.TrimSpace(log), "\n")) != 2 {
		t.Errorf("la rama del PR local debe quedar con un commit por tarea · %q", log)
	}
	desc, err := os.ReadFile(archivoPRLocal(root, RamaEntrega))
	if err != nil || !strings.Contains(string(desc), "T-002") {
		t.Errorf("falta la descripción del PR local · %v · %q", err, desc)
	}
}

// --integrar sin remoto avanza la base por fast-forward y limpia la rama,
// igual que un merge por rebase con --delete-branch en GitHub.
func TestEntregarTodasSinRemotoIntegraLocal(t *testing.T) {
	root, e := entregaLocal(t, true)

	if !e.Integrado {
		t.Fatalf("debió integrar en main · pasos %+v", e.Pasos)
	}
	if log := gitCmd(t, root, "log", "--format=%s", "main"); !strings.Contains(log, "T-001") || !strings.Contains(log, "T-002") {
		t.Errorf("main no tiene los commits de las tareas · %q", log)
	}
	if _, err := os.Stat(root + "/b.go"); err != nil {
		t.Error("main está en la carpeta de trabajo: el merge debió traer los archivos")
	}
	if _, err := gitRun(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+RamaEntrega); err == nil {
		t.Error("tras integrar, la rama de entrega sobra")
	}
}

// Con remoto y `entrega: local`, nada sale del repo: el PR queda local y
// --integrar avanza la base por fast-forward sin subir la rama.
func TestEntregarTodasLocalConRemotoNoSube(t *testing.T) {
	root := repoConCommit(t)
	remoto := t.TempDir()
	gitCmd(t, remoto, "init", "--bare", "-q")
	gitCmd(t, root, "remote", "add", "origin", remoto)
	gitCmd(t, root, "push", "-q", "origin", "main")

	_, e := entregaLocalEn(t, root, config.Config{Base: "main", Pruebas: "true", Entrega: config.EntregaLocal}, true)

	if !strings.HasPrefix(e.PR, PRLocal) || !e.Integrado {
		t.Fatalf("PR = %q, integrado = %v · debe ser local e integrado · pasos %+v", e.PR, e.Integrado, e.Pasos)
	}
	if ramas := gitCmd(t, remoto, "branch", "--list", "loom/*"); strings.TrimSpace(ramas) != "" {
		t.Errorf("la rama de entrega llegó al remoto · %q", ramas)
	}
	if local, enRemoto := gitCmd(t, root, "rev-parse", "main"), gitCmd(t, remoto, "rev-parse", "main"); local == enRemoto {
		t.Error("main local debió avanzar y el del remoto quedarse")
	}
}
