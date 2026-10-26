package spec

import (
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

// moduloConPuntos es un id de Python como lo recibe unittest o Django:
// "paquete.tests.test_algo" o "paquete.tests.test_algo.Clase.test_x".
var moduloConPuntos = regexp.MustCompile(`^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)+$`)

// AceptacionSinPrueba avisa de cada comando de aceptación que nombra una
// prueba que no está en el repo de root ni en el alcance de ninguna tarea.
// Esa aceptación solo puede fallar, y sin este aviso se descubre al final
// de la entrega, con todas las tareas ya pagadas. Si la prueba existe con
// otro prefijo ("tests.test_x" cuando vive en "apps/tests/test_x.py"), lo
// dice: es el error más común.
//
// Es advertencia y no error: un proyecto que mueve su sys.path puede
// importar un módulo desde otra raíz. Sin git no opina.
func AceptacionSinPrueba(root string, s Spec, tareas []task.Task) []Issue {
	c := exec.Command("git", "ls-files", "-co", "--exclude-standard")
	c.Dir = root
	salida, err := c.Output()
	if err != nil {
		return nil
	}
	rutas := strings.Split(strings.TrimSpace(string(salida)), "\n")
	for _, t := range tareas {
		rutas = append(rutas, t.TocarSolo...)
	}
	existe := func(p string) bool {
		for _, r := range rutas {
			if r == p || strings.HasPrefix(r, p+"/") {
				return true
			}
			if ok, _ := path.Match(r, p); ok {
				return true
			}
		}
		return false
	}
	var out []Issue
	for _, cmd := range s.AcceptanceCommands() {
		for _, tkn := range strings.Fields(cmd) {
			tkn = strings.TrimPrefix(strings.Trim(tkn, "()\"';"), "./")
			nombrada, candidatas := pruebaNombrada(tkn)
			if nombrada == "" || slicesAny(candidatas, existe) {
				continue
			}
			msg := fmt.Sprintf("la aceptación `%s` nombra %s, que no existe y ninguna tarea escribe", cmd, tkn)
			for _, r := range rutas {
				if strings.HasSuffix(r, "/"+nombrada) {
					msg = fmt.Sprintf("la aceptación `%s` nombra %s, que no existe: la prueba está en %s · corrige la ruta en la aceptación", cmd, tkn, r)
					break
				}
			}
			out = append(out, Issue{"warning", "acceptance_missing_test", msg})
		}
	}
	return out
}

// pruebaNombrada devuelve el archivo de prueba que nombra un token de un
// comando y las rutas con que se daría por existente; "" si no nombra una.
func pruebaNombrada(tkn string) (archivo string, candidatas []string) {
	if strings.ContainsAny(tkn, "*?[$") {
		return "", nil
	}
	if strings.Contains(tkn, "/") && task.EsArchivoDePrueba(tkn) {
		archivo, _, _ = strings.Cut(tkn, "::") // pytest: ruta::prueba
		return archivo, []string{archivo}
	}
	if !moduloConPuntos.MatchString(tkn) {
		return "", nil
	}
	// cada prefijo que termina en test_* puede ser el módulo (lo que
	// sigue son la clase y el método) o un paquete; el más corto es el
	// que se busca con otro prefijo
	partes := strings.Split(tkn, ".")
	for n := 1; n <= len(partes); n++ {
		if !strings.HasPrefix(partes[n-1], "test_") {
			continue
		}
		ruta := strings.Join(partes[:n], "/")
		if archivo == "" {
			archivo = ruta + ".py"
		}
		candidatas = append(candidatas, ruta+".py", ruta)
	}
	return archivo, candidatas
}

func slicesAny(xs []string, f func(string) bool) bool {
	for _, x := range xs {
		if f(x) {
			return true
		}
	}
	return false
}
