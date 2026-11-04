package historial

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// TrailerBase anota en cada release de qué commit de la base salió: la
// siguiente resume solo lo que vino después.
const TrailerBase = "Loom-Base"

// maxLineas topa cada sección del resumen: la release se lee, no se audita.
const maxLineas = 15

// Release es lo que Publicar dejó en la rama de releases.
type Release struct {
	Tag          string
	Rama         string
	Commit       string
	Mejoras      []string
	Reparaciones []string
}

// Avanzar lleva la rama actual hasta la de entrega si va detrás de ella
// en línea recta. Sin remoto, la entrega queda en una rama local y el
// archivo se commitea en la base: sin este avance hacía falta un merge a
// mano, que dejaba un commit de merge por versión.
func Avanzar(root, entrega string) (bool, error) {
	if entrega == "" || salida(root, "rev-parse", "--verify", "-q", entrega) == "" {
		return false, nil
	}
	if salida(root, "rev-parse", "HEAD") == salida(root, "rev-parse", entrega) {
		return false, nil
	}
	if git(root, "merge-base", "--is-ancestor", "HEAD", entrega) != nil {
		return false, nil
	}
	if err := git(root, "merge", "--ff-only", "-q", entrega); err != nil {
		return false, fmt.Errorf(idioma.T("no se pudo avanzar la base hasta %s · %w"), entrega, err)
	}
	return true, nil
}

// Publicar deja en la rama de releases un solo commit con el árbol de la
// rama actual, su tag de versión y un resumen de lo que trae, sacado de
// los Conventional Commits desde la release anterior. La base puede tener
// cualquier historial; la de releases queda con una línea por versión. No
// toca el árbol de trabajo ni sube nada.
func Publicar(root, rama, feature, motivo string) (Release, error) {
	r := Release{Rama: rama}
	if err := PuedePublicar(root, rama); err != nil {
		return r, err
	}
	head := salida(root, "rev-parse", "HEAD")
	previo := salida(root, "rev-parse", "--verify", "-q", "refs/heads/"+rama)

	desde := ""
	if previo != "" {
		desde = strings.TrimSpace(salida(root, "log", "-1", "--format=%(trailers:key="+TrailerBase+",valueonly)", previo))
		if desde == "" || git(root, "merge-base", "--is-ancestor", desde, head) != nil {
			desde = salida(root, "merge-base", previo, head)
		}
	}
	rango := head
	if desde != "" {
		rango = desde + ".." + head
	}
	var rompe bool
	for _, s := range strings.Split(salida(root, "log", "--no-merges", "--format=%s", rango), "\n") {
		m := reConvencional.FindStringSubmatch(strings.TrimSpace(s))
		if m == nil {
			continue
		}
		rompe = rompe || m[3] == "!"
		switch m[1] {
		case "feat":
			r.Mejoras = append(r.Mejoras, m[4])
		case "fix":
			r.Reparaciones = append(r.Reparaciones, m[4])
		}
	}

	ultima := ""
	if previo != "" {
		ultima = salida(root, "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", previo)
	}
	r.Tag = siguienteVersion(ultima, rompe, len(r.Mejoras) > 0)
	if salida(root, "rev-parse", "--verify", "-q", "refs/tags/"+r.Tag) != "" {
		return r, fmt.Errorf(idioma.T("el tag %s ya existe · bórralo o crea la release a mano"), r.Tag)
	}

	msg := mensajeRelease(r, feature, motivo, head)
	args := []string{"commit-tree", head + "^{tree}"}
	if previo != "" {
		args = append(args, "-p", previo)
	}
	cmd := exec.Command("git", append(args, "-F", "-")...)
	cmd.Dir, cmd.Stdin = root, strings.NewReader(msg)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	out, err := cmd.Output()
	if err != nil {
		return r, fmt.Errorf(idioma.T("no se pudo crear el commit de la release · %s"), strings.TrimSpace(errb.String()))
	}
	r.Commit = strings.TrimSpace(string(out))
	// con el valor viejo: si alguien movió la rama mientras tanto, no se pisa
	if err := git(root, "update-ref", "refs/heads/"+rama, r.Commit, previo); err != nil {
		return r, fmt.Errorf(idioma.T("no se pudo mover %s · %w"), rama, err)
	}
	if err := git(root, "tag", "-a", r.Tag, "-m", msg, r.Commit); err != nil {
		return r, fmt.Errorf(idioma.T("release en %s pero el tag %s falló · %w"), rama, r.Tag, err)
	}
	return r, nil
}

// PuedePublicar dice si la rama de releases se puede mover sin pisar a
// nadie. archive lo pregunta antes de archivar: después el spec ya está
// vacío y no habría cómo reintentar.
func PuedePublicar(root, rama string) error {
	if salida(root, "symbolic-ref", "--short", "-q", "HEAD") == rama {
		return fmt.Errorf(idioma.T("estás en %s, la rama de releases · archiva desde la base"), rama)
	}
	if otro := checadaEn(root, rama); otro != "" {
		return fmt.Errorf(idioma.T("%s está abierta en %s · cámbiala de rama allá y archiva otra vez"), rama, otro)
	}
	return nil
}

var (
	reConvencional = regexp.MustCompile(`^(\w+)(\([^)]*\))?(!)?: (.+)$`)
	reVersion      = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
)

// siguienteVersion sube el major si algo rompe (el minor antes de 1.0),
// el minor si hay mejoras y el patch si solo hay arreglos. Sin release
// previa, v0.1.0.
func siguienteVersion(ultima string, rompe, mejoras bool) string {
	m := reVersion.FindStringSubmatch(strings.TrimSpace(ultima))
	if m == nil {
		return "v0.1.0"
	}
	x, _ := strconv.Atoi(m[1])
	y, _ := strconv.Atoi(m[2])
	z, _ := strconv.Atoi(m[3])
	switch {
	case rompe && x > 0:
		return fmt.Sprintf("v%d.0.0", x+1)
	case rompe || mejoras:
		return fmt.Sprintf("v%d.%d.0", x, y+1)
	}
	return fmt.Sprintf("v%d.%d.%d", x, y, z+1)
}

func mensajeRelease(r Release, feature, motivo, base string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "release: %s · %s\n\n", r.Tag, feature)
	if l, _, _ := strings.Cut(strings.TrimSpace(motivo), "\n"); l != "" {
		b.WriteString(l + "\n\n")
	}
	seccion := func(titulo string, ls []string) {
		if len(ls) == 0 {
			return
		}
		b.WriteString(titulo + ":\n")
		for i, l := range ls {
			if i == maxLineas {
				fmt.Fprintf(&b, "- … +%d\n", len(ls)-maxLineas)
				break
			}
			b.WriteString("- " + l + "\n")
		}
		b.WriteString("\n")
	}
	seccion(idioma.T("Mejoras"), r.Mejoras)
	seccion(idioma.T("Reparaciones"), r.Reparaciones)
	fmt.Fprintf(&b, "%s: %s\n", TrailerBase, base)
	return b.String()
}

// checadaEn es el worktree donde la rama está abierta, salvo el actual:
// moverla por debajo dejaría ese árbol desfasado.
func checadaEn(root, rama string) string {
	aqui := salida(root, "rev-parse", "--show-toplevel")
	dir := ""
	for _, l := range strings.Split(salida(root, "worktree", "list", "--porcelain"), "\n") {
		if d, ok := strings.CutPrefix(l, "worktree "); ok {
			dir = d
		}
		if l == "branch refs/heads/"+rama && dir != aqui {
			return dir
		}
	}
	return ""
}

func salida(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
