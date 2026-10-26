package ship

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// con esqueleto la suite sigue roja hasta rellenar todos los módulos:
// una tarea no puede quedar frenada por los stubs de las demás, pero
// sí por empeorar una suite que al empezar estaba verde
func TestSuiteYaFallabaMiraLaBaseYVuelveALaRama(t *testing.T) {
	root := repoConCommit(t)
	escribir(t, root, "rojo", "")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "stub")
	base := strings.TrimSpace(gitCmd(t, root, "rev-parse", "HEAD"))
	r := cuartoConWip(t, root)

	suite := "test ! -e rojo"
	if !suiteYaFallaba(context.Background(), r.Path, base, suite, time.Minute) {
		t.Fatal("la suite fallaba en la base y no se vio")
	}
	if rama := strings.TrimSpace(gitCmd(t, r.Path, "rev-parse", "--abbrev-ref", "HEAD")); rama != r.Rama {
		t.Fatalf("el cuarto quedó en %q", rama)
	}
	if suiteYaFallaba(context.Background(), r.Path, base, "true", time.Minute) {
		t.Fatal("una suite verde en la base no justifica relajar la esclusa")
	}
}

// la suite fallaba en la rama de T-048 por un timeout que main ya
// había corregido. En la entrega conjunta basta su listo_cuando; suelta,
// la tarea sigue exigiendo la suite
func TestBisectableEnEntregaConjuntaSeConformaConListoCuando(t *testing.T) {
	dir := t.TempDir()
	o := Opciones{
		Room:    room.Room{Path: dir},
		Task:    task.Task{ListoCuando: "true"},
		Config:  config.Config{Pruebas: "false"},
		Timeout: time.Minute,
	}
	if _, ok := bisectable(context.Background(), o); ok {
		t.Fatal("suelta, una suite roja tiene que frenar")
	}
	o.SuiteAlIntegrar = true
	if detalle, ok := bisectable(context.Background(), o); !ok {
		t.Fatalf("en la entrega conjunta debió bastar el listo_cuando: %s", detalle)
	}
	o.Task.ListoCuando = "false"
	if _, ok := bisectable(context.Background(), o); ok {
		t.Fatal("con el listo_cuando rojo tiene que frenar igual")
	}
}
