package overlap

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// TimeoutSuitePorDefecto es lo que se le da a cada suite sobre la fusión
// cuando la configuración no dice otra cosa.
const TimeoutSuitePorDefecto = 10 * time.Minute

// Suite es lo que el nivel funcional necesita de una tarea: quién es y
// cuál es el comando que la declara verde.
type Suite struct {
	ID          string
	ListoCuando string
}

// ResultadoFuncional es el tercer nivel del solapamiento: qué pasó al correr las
// suites de las dos tareas sobre su fusión.
//
// Corrio y Rompen son preguntas distintas. Un par sin correr no es un par
// limpio: es un par del que no se sabe nada, y Motivo dice por qué.
type ResultadoFuncional struct {
	TareaA string   `json:"tarea_a"`
	TareaB string   `json:"tarea_b"`
	Corrio bool     `json:"corrio"`
	Rompen []string `json:"rompen,omitempty"`
	Salida string   `json:"salida,omitempty"`
	Motivo string   `json:"motivo,omitempty"`
}

// Alerta devuelve la línea para el humano, o "" si las dos suites
// pasaron sobre la fusión. Un par que no se pudo correr no calla: decir
// "limpio" sin haber mirado es justo lo que esta comprobación evita.
func (r ResultadoFuncional) Alerta() string {
	if !r.Corrio {
		if r.Motivo == "" {
			return ""
		}
		return fmt.Sprintf("%s ↔ %s · no se pudo comprobar la fusión: %s", r.TareaA, r.TareaB, r.Motivo)
	}
	if len(r.Rompen) == 0 {
		return ""
	}
	return fmt.Sprintf("%s ↔ %s · verdes por separado, pero al fusionarlas falla %s",
		r.TareaA, r.TareaB, strings.Join(r.Rompen, " y "))
}

// CheckFuncional monta la fusión de las dos ramas en un worktree suelto y
// corre ahí las suites de las dos tareas (nivel funcional).
//
// Es el nivel que atrapa el fallo que los otros dos no ven: dos ramas
// verdes por separado que rompen juntas. Sin conflicto de texto y sin
// símbolo en común, porque lo que cambió fue un comportamiento del que la
// otra dependía. También es el único que ejecuta código, así que el
// llamador lo dispara solo cuando ya hay sospecha (Resultado.Sospechoso)
// y solo entre tareas que quedaron verdes: una suite que ya fallaba en su
// propia rama no dice nada sobre la fusión.
//
// El árbol fusionado no se recalcula: merge-tree ya lo escribió en
// CheckPar y viene en res.Arbol. De ahí sale un commit suelto —con los
// dos padres, para que el worktree tenga un historial coherente— que se
// monta detached y se destruye al terminar. Nada de esto toca las ramas
// de las tareas ni el árbol de trabajo del repo.
func CheckFuncional(ctx context.Context, root string, res Resultado, a, b Suite, timeoutSuite time.Duration) ResultadoFuncional {
	rf := ResultadoFuncional{TareaA: a.ID, TareaB: b.ID}

	if res.Arbol == "" {
		// sin árbol no hay nada que montar: o chocaron en texto (y eso ya
		// lo reporta el nivel 1) o no había qué comparar todavía
		return rf
	}
	if a.ListoCuando == "" || b.ListoCuando == "" {
		rf.Motivo = "alguna de las dos tareas no declara listo_cuando"
		return rf
	}
	if timeoutSuite <= 0 {
		timeoutSuite = TimeoutSuitePorDefecto
	}

	commit, err := commitDeArbol(ctx, root, res.Arbol, room.Branch(a.ID), room.Branch(b.ID))
	if err != nil {
		rf.Motivo = err.Error()
		return rf
	}

	dir, err := os.MkdirTemp("", "loom-fusion-")
	if err != nil {
		rf.Motivo = err.Error()
		return rf
	}
	// worktree add exige que el directorio no exista; MkdirTemp lo crea.
	if err := os.Remove(dir); err != nil {
		rf.Motivo = err.Error()
		return rf
	}
	if out, err := git(ctx, root, "worktree", "add", "--detach", dir, commit); err != nil {
		rf.Motivo = "no se pudo montar la fusión · " + tail(out)
		return rf
	}
	defer func() {
		// contexto propio: si el de arriba ya se canceló, el worktree se
		// quedaría montado y el próximo `worktree add` chocaría con él
		limpiar, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = git(limpiar, root, "worktree", "remove", "--force", dir)
		_, _ = git(limpiar, root, "worktree", "prune")
		_ = os.RemoveAll(dir)
	}()

	if err := room.InstalarDependencias(ctx, dir); err != nil {
		rf.Motivo = err.Error()
		return rf
	}

	rf.Corrio = true
	var salidas []string
	for _, s := range []Suite{a, b} {
		ok, out := correrSuite(ctx, dir, s.ListoCuando, timeoutSuite)
		if !ok {
			rf.Rompen = append(rf.Rompen, s.ID)
			salidas = append(salidas, s.ID+" · "+tail(out))
		}
	}
	rf.Salida = strings.Join(salidas, "\n")
	return rf
}

// commitDeArbol envuelve el árbol fusionado en un commit con las dos
// ramas de padres. Va con identidad propia en la línea de comandos
// porque commit-tree la exige y el repo del usuario puede no tenerla
// configurada; es lo mismo que hace la esclusa de salida.
func commitDeArbol(ctx context.Context, root, arbol, ramaA, ramaB string) (string, error) {
	args := []string{
		"-c", "user.name=loom", "-c", "user.email=loom@local",
		"commit-tree", arbol, "-m", "loom: fusión en seco para el nivel funcional",
	}
	for _, rama := range []string{ramaA, ramaB} {
		sha, err := git(ctx, root, "rev-parse", "--verify", "--quiet", rama+"^{commit}")
		if err != nil {
			return "", fmt.Errorf("la rama %s no existe", rama)
		}
		args = append(args, "-p", strings.TrimSpace(sha))
	}
	out, err := git(ctx, root, args...)
	if err != nil {
		return "", fmt.Errorf("no se pudo escribir el commit de la fusión · %s", tail(out))
	}
	return strings.TrimSpace(out), nil
}

// correrSuite corre un listo_cuando dentro del worktree de la fusión.
// Devuelve si pasó y la salida, que es lo que el humano necesita para
// entender qué se rompió.
func correrSuite(ctx context.Context, dir, listoCuando string, timeout time.Duration) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", task.SinTerminal(listoCuando))
	cmd.Env = append(os.Environ(), room.Entorno(dir)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return false, "la suite se pasó del tiempo"
	}
	return err == nil, string(out)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func tail(s string) string {
	lineas := strings.Split(strings.TrimSpace(s), "\n")
	if len(lineas) > 5 {
		lineas = lineas[len(lineas)-5:]
	}
	return strings.Join(lineas, "\n")
}
