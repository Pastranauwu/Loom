// Package overlap implements the active overlap detection en sus
// tres niveles: textual (git merge-tree), semántico (símbolos exportados
// en común, de attempts.jsonl) y funcional (fusionar en seco y correr las
// suites de las dos tareas sobre el resultado, en funcional.go).
package overlap

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/room"
)

// Resultado is the overlap check result between two tasks.
type Resultado struct {
	TareaA     string   `json:"tarea_a"`
	TareaB     string   `json:"tarea_b"`
	Textual    bool     `json:"textual"`
	Semantico  bool     `json:"semantico"`
	Comunes    []string `json:"comunes,omitempty"`
	Conflictos []string `json:"conflictos,omitempty"`

	// Indeterminado lleva el motivo por el que la comprobacion textual
	// no se pudo hacer. Vacio no significa "limpio" salvo que Textual
	// tambien sea false: son tres estados, no dos.
	Indeterminado string `json:"indeterminado,omitempty"`

	// Arbol es el OID del árbol que deja la fusión en seco cuando sale
	// limpia. Lo calcula merge-tree de paso, así que el nivel funcional
	// lo hereda en vez de volver a fusionar. Vacío si hubo conflicto o
	// no se pudo comparar. No viaja en el JSON: es un detalle interno.
	Arbol string `json:"-"`
}

// Sospechoso reporta si este par amerita el nivel funcional, que es el
// único que cuesta caro porque ejecuta código. Un par limpio en texto y
// en símbolos no se merece dos suites: el tercer nivel se
// dispare solo cuando alguno de los dos primeros marcó algo.
func (r Resultado) Sospechoso() bool { return r.Textual || r.Semantico }

// Alerta returns a human-readable alert if any overlap was detected.
// Returns "" if clean.
func (r Resultado) Alerta() string {
	if !r.Textual && !r.Semantico && r.Indeterminado == "" {
		return ""
	}
	var partes []string
	if r.Indeterminado != "" {
		partes = append(partes, "no se pudo comprobar el cruce de texto: "+r.Indeterminado)
	}
	if r.Textual {
		partes = append(partes, "conflicto de texto en: "+strings.Join(r.Conflictos, ", "))
	}
	if r.Semantico {
		partes = append(partes, "símbolo(s) exportado(s) en común: "+strings.Join(r.Comunes, ", "))
	}
	return fmt.Sprintf("%s ↔ %s · %s", r.TareaA, r.TareaB, strings.Join(partes, "; "))
}

// CheckPar runs textual and semantic checks between two tasks.
// root is the repo root; attemptsA/B are the recorded attempts for each.
func CheckPar(root, idA, idB string, attemptsA, attemptsB []loop.Attempt) Resultado {
	res := Resultado{TareaA: idA, TareaB: idB}

	// semantic: shared exported symbols from last attempt
	res.Comunes = simbolosComunes(attemptsA, attemptsB)
	res.Semantico = len(res.Comunes) > 0

	// textual: git merge-tree between the two branches
	ramaA := room.Branch(idA)
	ramaB := room.Branch(idB)
	arbol, conflictos, err := mergeTree(root, ramaA, ramaB)
	if err != nil {
		res.Indeterminado = err.Error()
	}
	res.Arbol = arbol
	res.Conflictos = conflictos
	res.Textual = len(conflictos) > 0

	return res
}

// mergeTree runs git merge-tree and returns the merged tree OID and the
// conflicting file paths.
//
// Con --write-tree --no-messages la salida es el OID del árbol escrito y,
// cuando hay conflicto, una línea por etapa sin fusionar en formato
// "<modo> <oid> <etapa>\t<ruta>". Esas líneas son estables y no dependen
// del idioma de la terminal — los mensajes "CONFLICT (content)" sí (en
// español salen "CONFLICTO (contenido)"), y el parseo por prefijo no los
// encontraba: falso negativo que escondía el archivo en conflicto.
//
// El árbol solo sale cuando la fusión es limpia; con conflicto git no
// escribe uno utilizable. Es lo que el nivel funcional monta para correr
// las suites, y por eso se devuelve aunque a nadie más le importe.
//
// Tres estados, no dos:
//
//   - arbol, nil, nil     fusion limpia (arbol vacio si no habia nada que
//     comparar: una rama que aun no existe no es un conflicto, era el
//     falso positivo "loom/T-001 ↔ loom/T-002" de antes del
//     primer wip:)
//   - "", conflictos, nil conflicto real, con las rutas
//   - "", nil, err        NO se pudo comparar, y hay que decirlo
//
// El tercero existía y se reportaba como el primero. --write-tree llegó
// en git 2.38; en Ubuntu 22.04 LTS (git 2.34) el flag no existe, git sale
// con 129 y la deteccion textual quedaba apagada sin que nadie avisara.
func mergeTree(root, ramaA, ramaB string) (string, []string, error) {
	cmd := exec.Command("git", "-C", root, "merge-tree", "--write-tree", "--no-messages", ramaA, ramaB)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		// fusión limpia: la primera línea es el OID del árbol escrito
		arbol, _, _ := strings.Cut(strings.TrimSpace(stdout.String()), "\n")
		return strings.TrimSpace(arbol), nil, nil
	}
	var exitErr *exec.ExitError
	if !isExitError(err, &exitErr) {
		return "", nil, fmt.Errorf("no se pudo ejecutar git merge-tree · %w", err)
	}
	if esGitSinWriteTree(exitErr.ExitCode(), stderr.String()) {
		return "", nil, errGitViejo
	}
	if exitErr.ExitCode() != 1 {
		return "", nil, nil // no es un conflicto: no habia que comparar
	}
	// exit 1 puede ser conflicto real o "no se pudo fusionar" (rama
	// inexistente). Lo distingue la salida: un conflicto trae líneas de
	// etapa; un error trae un mensaje y nada más.
	var conflictos []string
	seen := map[string]bool{}
	for _, line := range strings.Split(stdout.String(), "\n") {
		ruta, ok := parseLineaEtapa(line)
		if ok && !seen[ruta] {
			conflictos = append(conflictos, ruta)
			seen[ruta] = true
		}
	}
	return "", conflictos, nil
}

// errGitViejo: el git del sistema no entiende merge-tree --write-tree.
var errGitViejo = errors.New("git no soporta 'merge-tree --write-tree' · necesita git 2.38 o superior (Ubuntu 22.04 trae 2.34)")

// esGitSinWriteTree reconoce el fallo por flag desconocido. git sale con
// 129 en error de uso, pero se comprueba tambien el stderr porque el
// codigo solo no distingue "flag que no existe" de otros usos malos.
func esGitSinWriteTree(code int, stderr string) bool {
	if code != 129 {
		return false
	}
	s := strings.ToLower(stderr)
	return strings.Contains(s, "unknown option") ||
		strings.Contains(s, "usage:") ||
		strings.Contains(s, "opción desconocida") ||
		strings.Contains(s, "uso:")
}

// parseLineaEtapa extrae la ruta de una línea de etapa sin fusionar de
// merge-tree: "<modo> <oid> <etapa>\t<ruta>". La etapa (1 base, 2 la
// nuestra, 3 la suya) es lo que la distingue del resto de la salida.
func parseLineaEtapa(line string) (string, bool) {
	meta, ruta, ok := strings.Cut(line, "\t")
	if !ok || ruta == "" {
		return "", false
	}
	campos := strings.Fields(meta)
	if len(campos) != 3 {
		return "", false
	}
	switch campos[2] {
	case "1", "2", "3":
		return ruta, true
	}
	return "", false
}

func isExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

func simbolosComunes(attemptsA, attemptsB []loop.Attempt) []string {
	symA := ultimosSimbolos(attemptsA)
	symB := ultimosSimbolos(attemptsB)
	if len(symA) == 0 || len(symB) == 0 {
		return nil
	}
	setA := make(map[string]bool, len(symA))
	for _, s := range symA {
		setA[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range symB {
		if setA[s] && !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

func ultimosSimbolos(as []loop.Attempt) []string {
	for i := len(as) - 1; i >= 0; i-- {
		if as[i].SimbolosExportados != nil {
			return *as[i].SimbolosExportados
		}
	}
	return nil
}

// SoportaWriteTree reporta si el git del sistema entiende
// `merge-tree --write-tree`, de la que depende la deteccion textual de
// solapamiento. `doctor` lo usa para avisar antes de una corrida en vez
// de dejar el chequeo apagado sin decirlo.
func SoportaWriteTree() bool {
	cmd := exec.Command("git", "merge-tree", "--write-tree", "-h")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && isExitError(err, &exitErr) {
		return !esGitSinWriteTree(exitErr.ExitCode(), stderr.String())
	}
	return true
}

// VersionGit devuelve la version del git del sistema, o "" si no se pudo leer.
func VersionGit() string {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return ""
	}
	campos := strings.Fields(string(out))
	if len(campos) < 3 {
		return ""
	}
	return campos[2]
}
