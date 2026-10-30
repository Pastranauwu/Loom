package loop

import (
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

// revertFueraDeAlcance revierte los archivos que el agente tocó fuera de
// tocar_solo y los archivos de prueba, y devuelve la lista
// de lo revertido para anotarlo en attempts.jsonl. La reversión es el
// enforcement real de los límites: la verificación la hace el código, no
// la confianza en el modelo.
//
// antes es el commit con que arrancó el intento, y es contra él que se
// mide y se restaura. Mirar solo `git status` dejaba la reversión ciega
// cuando el agente commitea por su cuenta dentro del cuarto (la skill
// `implement` lo hace): con el árbol limpio no había nada que revertir y
// las pruebas que el propio agente se escribió —o cualquier archivo
// fuera de alcance— viajaban al PR. Vacío cae de vuelta a HEAD.
func revertFueraDeAlcance(roomPath, antes string, tocarSolo, patrones []string) ([]string, error) {
	revertidos, _, err := revertirConParche(roomPath, antes, tocarSolo, patrones)
	return revertidos, err
}

// revertirConParche es revertFueraDeAlcance y además devuelve, como
// parche de git, lo que el agente cambió fuera de su alcance en archivos
// que no son prueba. Es lo que la tarea necesitó y no era suyo: casi
// siempre algo roto en la base, que el agente arregla y la reversión
// deshace en cada intento. Se guarda para que el humano lo aplique donde va.
func revertirConParche(roomPath, antes string, tocarSolo, patrones []string) ([]string, string, error) {
	files, err := statusFiles(roomPath)
	if err != nil {
		return nil, "", err
	}
	if antes != "" {
		commiteados, err := filesSince(roomPath, antes)
		if err != nil {
			return nil, "", err
		}
		files = unir(files, commiteados)
	}
	fuente := antes
	if fuente == "" {
		fuente = "HEAD"
	}
	var revertidos []string
	var parche strings.Builder
	for _, f := range files {
		if enAlcance(f, tocarSolo) && !esPrueba(f, patrones) {
			continue
		}
		if !esPrueba(f, patrones) {
			d, _ := gitRun(roomPath, "diff", fuente, "--", f)
			if d == "" {
				// sin seguimiento: diff no lo ve; --no-index sale con 1 y el parche
				d, _ = gitRun(roomPath, "diff", "--no-index", "--", os.DevNull, f)
			}
			parche.WriteString(d)
		}
		if err := revertir(roomPath, antes, f); err != nil {
			return revertidos, parche.String(), err
		}
		revertidos = append(revertidos, f)
	}
	return revertidos, parche.String(), nil
}

// enAlcance reporta si un archivo cae dentro de tocar_solo. Vacío
// significa "sin restricción": todo el repo salvo las pruebas.
//
// Los lockfiles derivados de un manifiesto en alcance cuentan como
// dentro: revertirlos dejaba el proyecto sin poder compilar (ver
// config.LockfilesDerivados).
func enAlcance(s string, tocarSolo []string) bool {
	if len(tocarSolo) == 0 {
		return true
	}
	if config.MatchesAny(tocarSolo, s) {
		return true
	}
	for _, lock := range config.LockfilesDerivados(tocarSolo) {
		if s == lock || path.Base(s) == lock {
			return true
		}
	}
	return false
}

func esPrueba(s string, patrones []string) bool {
	return config.MatchesAny(patrones, s)
}

// unir junta dos listas de rutas sin repetir y en orden estable, para
// que la lista de revertidos no dependa del orden en que git las liste.
func unir(a, b []string) []string {
	vistos := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, l := range [][]string{a, b} {
		for _, s := range l {
			if s == "" || vistos[s] {
				continue
			}
			vistos[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// revertir devuelve un archivo al estado con que arrancó el intento, o lo
// borra si ahí no existía (lo creó el agente). Borrarlo del árbol basta:
// el bucle indexa con `git add -A` justo después, así que el commit de
// respaldo registra la baja aunque el agente lo hubiera commiteado.
func revertir(roomPath, antes, s string) error {
	fuente := antes
	if fuente == "" {
		fuente = "HEAD"
	}
	if _, err := gitRun(roomPath, "cat-file", "-e", fuente+":"+s); err != nil {
		// no existía en el punto de partida: lo creó el agente
		if err := os.Remove(filepath.Join(roomPath, s)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf(idioma.T("no se pudo borrar %s · %s"), s, err)
		}
		return nil
	}
	if _, err := gitRun(roomPath, "restore", "--source="+fuente, "--", s); err != nil {
		return fmt.Errorf(idioma.T("no se pudo revertir %s · %s"), s, err)
	}
	return nil
}

// restaurarPruebasBorradas devuelve al cuarto los archivos de prueba que
// ya existían en desde (el arranque del cuarto) y el intento borró, si la
// tarea no los nombra por ruta exacta ni declara "Obsoleto:". Aun cuando
// el agente es dueño de sus pruebas, una prueba vieja borrada entera
// era el rechazo más común del revisor: se atrapa aquí, sin modelo.
func restaurarPruebasBorradas(roomPath, antes, desde string, t task.Task) []string {
	if antes == "" || desde == "" || idioma.Tiene(t.Notas, idioma.Obsoleto) {
		return nil
	}
	borrados, err := gitRun(roomPath, "diff", "--name-only", "--diff-filter=D", antes, "--")
	if err != nil {
		return nil
	}
	estado, _ := gitRun(roomPath, "ls-files", "--deleted")
	patrones := config.DefaultTestPatterns()
	var out []string
	for _, f := range unir(strings.Split(borrados, "\n"), strings.Split(estado, "\n")) {
		if f == "" || !esPrueba(f, patrones) || slices.Contains(t.TocarSolo, f) {
			continue
		}
		if _, err := gitRun(roomPath, "cat-file", "-e", desde+":"+f); err != nil {
			continue // la escribió el agente en un intento anterior: es suya
		}
		if _, err := gitRun(roomPath, "checkout", desde, "--", f); err == nil {
			out = append(out, f)
		}
	}
	return out
}
