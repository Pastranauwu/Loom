package loop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LogPath devuelve la ruta del volcado de una invocación del agente.
func LogPath(root, id string, intento int) string {
	return filepath.Join(RunsDir(root), id, fmt.Sprintf("intento-%d.log", intento))
}

// guardarExamen deja por escrito por qué el examinador ciego no dejó
// suite. Sin esto la tarea moría con "no tiene suite que lo juzgue" y
// nadie podía saber si el modelo no respondió, si la suite no compilaba o
// si no se resolvieron sus imports.
func guardarExamen(root, id string, err error) {
	p := filepath.Join(RunsDir(root), id, "examinador.log")
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	ignorarLogs(root)
	_ = os.WriteFile(p, []byte(err.Error()+"\n"), 0o644)
}

// guardarLog vuelca prompt, stdout y stderr del CLI de agente a disco.
// Es lo único que permite responder "¿qué falló?" sin volver a correr la
// tarea: el bucle solo se queda con un resumen, y un resumen no alcanza
// para depurar una invocación que ni siquiera llegó al modelo.
func guardarLog(root, id string, intento int, prompt string, res Result, agentErr error) string {
	p := LogPath(root, id, intento)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return ""
	}
	ignorarLogs(root)
	var b strings.Builder
	fmt.Fprintf(&b, "=== intento %d · salida %d ===\n", intento, res.ExitCode)
	if agentErr != nil {
		fmt.Fprintf(&b, "=== error ===\n%s\n", agentErr)
	}
	fmt.Fprintf(&b, "=== prompt ===\n%s\n", prompt)
	fmt.Fprintf(&b, "=== stdout ===\n%s\n", res.Stdout)
	fmt.Fprintf(&b, "=== stderr ===\n%s\n", res.Stderr)
	if os.WriteFile(p, []byte(b.String()), 0o644) != nil {
		return ""
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

// falloDeInfra reporta si la invocación ni siquiera llegó al modelo:
// salió con error, no gastó tokens y no tocó un solo archivo. Reintentar
// eso es quemar los intentos de la tarea contra un modelo mal escrito o
// una key ausente, que es exactamente lo que hacía que una corrida
// entera muriera en segundos sin dejar rastro.
func falloDeInfra(res Result, agentErr error, archivos []string) bool {
	return agentErr != nil &&
		res.Tokens.Entrada+res.Tokens.Salida == 0 &&
		len(archivos) == 0
}

// diagnostico saca el motivo más útil que dejó el CLI: primero stderr,
// luego la última línea de stdout (los CLIs en modo JSON reportan ahí
// sus errores de servidor), y como último recurso el error del proceso.
func diagnostico(res Result, agentErr error) string {
	if d := recorte(res.Stderr); d != "" {
		return d
	}
	if d := recorte(res.Stdout); d != "" {
		return d
	}
	if agentErr != nil {
		return agentErr.Error()
	}
	return "sin salida"
}

// recorte devuelve la última línea no vacía de s, acotada.
func recorte(s string) string {
	lineas := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lineas) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lineas[i])
		if l == "" {
			continue
		}
		const max = 400
		if len(l) > max {
			l = l[:max] + "…"
		}
		return l
	}
	return ""
}

// ignorarLogs deja un .gitignore en runs/ para que los volcados no entren
// al repo. attempts.jsonl sí se versiona (es la fuente de las métricas);
// el stdout crudo del CLI son cientos de KB por intento y solo sirve para
// depurar en caliente. Se escribe cada vez que hace falta, así los repos
// que ya tienen .loom/ también quedan cubiertos.
func ignorarLogs(root string) {
	p := filepath.Join(RunsDir(root), ".gitignore")
	if _, err := os.Stat(p); err == nil {
		return
	}
	_ = os.WriteFile(p, []byte("*/intento-*.log\n"), 0o644)
}
