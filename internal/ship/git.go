package ship

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

// sinPreguntas es el entorno con que corre todo git de la entrega: una
// credencial que el remoto rechaza tiene que fallar, no esperar a alguien.
// GIT_TERMINAL_PROMPT solo no alcanza: el askpass de un IDE abre su
// diálogo en otra ventana y el proceso queda colgado sin decir nada.
func sinPreguntas() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=")
}

// gitRun ejecuta git en dir y devuelve la salida combinada.
func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.askPass="}, args...)...)
	cmd.Dir = dir
	cmd.Env = sinPreguntas()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// traer actualiza origin. Es un paso de cortesía que se ignora si falla,
// así que tampoco espera a una red que no contesta.
func traer(root string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-c", "core.askPass=", "fetch", "--quiet")
	cmd.Dir = root
	cmd.Env = sinPreguntas()
	_ = cmd.Run()
}

// rebase trae la base y rebasea la rama del cuarto sobre ella. Con local
// (prLocal) no toca la red y va sobre la base local, que es de donde sale
// la rama de entrega. Devuelve la ref destino y, si hubo conflicto, los
// archivos implicados.
func rebase(ctx context.Context, root, roomPath, base, rama string, local bool) (target string, conflictos []string, err error) {
	target = base
	if !local {
		// sin remoto no hay qué traer: el fetch falla y se ignora
		traer(root)
		if _, err := gitRun(root, "rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
			target = "origin/" + base
		}
	}

	if _, err := gitRun(roomPath, "rebase", target); err != nil {
		conflictos = unmerged(roomPath)
		_, _ = gitRun(roomPath, "rebase", "--abort")
		return target, conflictos, err
	}
	return target, nil, nil
}

// unmerged lista los archivos en conflicto (diff-filter U).
func unmerged(dir string) []string {
	out, _ := gitRun(dir, "diff", "--name-only", "--diff-filter=U")
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files
}

// aplanar colapsa los commits wip de la rama en uno solo con mensaje
// Conventional Commits y trailer `Agent:`. Devuelve cuántos wip
// había y el hash del commit resultante.
func aplanar(ctx context.Context, roomPath, target, id, titulo, tipo, modelo string) (int, string, error) {
	out, err := gitRun(roomPath, "rev-list", "--count", target+"..HEAD")
	if err != nil {
		return 0, "", err
	}
	cuenta, _ := strconv.Atoi(strings.TrimSpace(out))

	if _, err := gitRun(roomPath, "reset", "--soft", target); err != nil {
		return 0, "", err
	}
	if _, err := gitRun(roomPath, "diff", "--cached", "--quiet"); err == nil {
		return 0, "", &noEntregaError{}
	}

	args := append(identity(roomPath), "commit", "-m", tipo+": "+titulo)
	var trailers []string
	if modelo != "" {
		trailers = append(trailers, "Agent: "+modelo)
	}
	if id != "" {
		trailers = append(trailers, TrailerTarea+": "+id)
	}
	if len(trailers) > 0 {
		args = append(args, "-m", strings.Join(trailers, "\n"))
	}
	if _, err := gitRun(roomPath, args...); err != nil {
		return 0, "", err
	}
	hash, err := gitRun(roomPath, "rev-parse", "HEAD")
	return cuenta, strings.TrimSpace(hash), err
}

// noEntregaError es el caso "nada que entregar": no hay cambios.
type noEntregaError struct{}

func (noEntregaError) Error() string { return "nada que entregar · la rama no tiene cambios" }

// identity devuelve los -c de identidad cuando el repo no tiene una
// configurada. Hacen falta LAS DOS: git deduce el correo del sistema
// cuando falta, pero no el nombre, y entonces cualquier commit muere con
// "empty ident name (for <user@host>) not allowed". Es lo que pasa en un
// runner de CI limpio, donde nadie corrió `git config user.name`.
func identity(roomPath string) []string {
	nombre, _ := gitRun(roomPath, "config", "user.name")
	email, _ := gitRun(roomPath, "config", "user.email")
	if strings.TrimSpace(nombre) == "" || strings.TrimSpace(email) == "" {
		return []string{"-c", "user.name=loom", "-c", "user.email=loom@local"}
	}
	return nil
}

// diffAplanado devuelve el texto, los archivos y las líneas del commit
// aplanado frente a la base. Alimenta a los escáneres.
func diffAplanado(roomPath, target string, patronesPrueba []string) (texto string, archivos []string, mas, menos, masPrueba int, err error) {
	texto, err = gitRun(roomPath, "diff", target+"..HEAD")
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	archivos, err = diffArchivos(roomPath, target)
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	mas, menos, masPrueba, err = diffNumstat(roomPath, target, patronesPrueba)
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	return texto, archivos, mas, menos, masPrueba, nil
}

func diffArchivos(roomPath, target string) ([]string, error) {
	out, err := gitRun(roomPath, "diff", "--name-only", target+"..HEAD")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			files = append(files, l)
		}
	}
	return files, nil
}

// diffNumstat cuenta las líneas del diff y separa las que caen en rutas
// de prueba.
//
// El presupuesto de una tarea lo estima un modelo pensando en el código
// de la solución, pero el diff trae además las pruebas — y en go y
// python las escribe el examinador ciego, no el agente. Cobrárselas
// hacía que casi toda tarea con examen se pasara del límite por el doble
// de lo estimado, y frenaba la entrega de trabajo correcto.
func diffNumstat(roomPath, target string, patronesPrueba []string) (mas, menos, masPrueba int, err error) {
	out, err := gitRun(roomPath, "diff", "--numstat", target+"..HEAD")
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		a, errA := strconv.Atoi(fields[0])
		d, errD := strconv.Atoi(fields[1])
		if errA != nil || errD != nil {
			continue
		}
		mas += a
		menos += d
		if config.MatchesAny(patronesPrueba, fields[2]) {
			masPrueba += a
		}
	}
	return mas, menos, masPrueba, nil
}

// tipoCommit decide el tipo Conventional Commits por lo que el commit
// toca: solo pruebas es test, solo documentación es docs. fix es solo
// cuando el título EMPIEZA por un verbo de arreglo. Antes bastaba una
// palabra en cualquier parte del título y en una corrida real, "pantalla de
// arregla el beat" (el nombre del juego), "romper beat determinista" y
// el esqueleto salieron como fix, y la prueba de integración como feat.
func tipoCommit(titulo string, archivos, patronesPrueba []string) string {
	pruebas, docs := len(archivos) > 0, len(archivos) > 0
	for _, a := range archivos {
		pruebas = pruebas && (task.EsArchivoDePrueba(a) || config.MatchesAny(patronesPrueba, a))
		docs = docs && esDocumentacion(a)
	}
	switch {
	case pruebas:
		return "test"
	case docs:
		return "docs"
	}
	primera, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(titulo)), " ")
	for _, w := range []string{"arregl", "fix", "corrig", "corrij", "repar", "solucion"} {
		if strings.HasPrefix(primera, w) {
			return "fix"
		}
	}
	return "feat"
}

// unir junta una lista en un texto legible.
func unir(items []string) string {
	return strings.Join(items, ", ")
}

// itoa convierte un entero sin strconv en cada llamada.
func itoa(n int) string { return strconv.Itoa(n) }
