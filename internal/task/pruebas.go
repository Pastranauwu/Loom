package task

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ArchivosDePrueba extrae de un comando los archivos de prueba que
// menciona: "npx vitest run src/core/types" → src/core/types.test.ts no
// está escrito, pero vitest con un path sin extensión busca el test
// compañero; "node --test test/validator.test.js" → test/validator.test.js
// sí está escrito. El objetivo es el que importa: que el agente pueda
// crear exactamente el archivo que listo_cuando va a correr.
func ArchivosDePrueba(cmd string) []string {
	var out []string
	for _, tkn := range strings.Fields(cmd) {
		if !parecePath(tkn) {
			continue
		}
		if EsArchivoDePrueba(tkn) {
			out = append(out, tkn)
			continue
		}
		// "vitest run src/core/types" corre el test compañero del módulo:
		// src/core/types.test.ts / .spec.ts. Es el patrón más repetido y
		// el que dejaba las tareas Node sin suite. Solo para paths sin
		// extensión que apunten a un módulo, no a un archivo concreto.
		if filepath.Ext(tkn) == "" {
			for _, suf := range []string{".test.ts", ".spec.ts", ".test.js", ".spec.js", ".test.mjs"} {
				out = append(out, tkn+suf)
			}
		}
	}
	return out
}

// parecePath reporta si un token de un comando puede ser una ruta de
// archivo: tiene slash o extensión de archivo. Los flags (--coverage),
// los verbos (npm, run, node) y los operadores (&&, |) no cuentan.
func parecePath(tkn string) bool {
	if tkn == "" || strings.HasPrefix(tkn, "-") || strings.HasPrefix(tkn, "&") ||
		strings.HasPrefix(tkn, "|") || strings.HasPrefix(tkn, ">") || strings.HasPrefix(tkn, "<") {
		return false
	}
	if strings.Contains(tkn, "/") {
		return true
	}
	ext := filepath.Ext(tkn)
	return ext == ".ts" || ext == ".js" || ext == ".mjs" || ext == ".py" || ext == ".go" || ext == ".rs"
}

// EsArchivoDePrueba reporta si una ruta es un archivo de prueba por
// convención de nombres: .test.ts, .spec.ts, test_*.py, *_test.go, etc.
func EsArchivoDePrueba(p string) bool {
	b := strings.ToLower(filepath.Base(p))
	ext := filepath.Ext(p)
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return strings.Contains(b, ".test.") || strings.Contains(b, ".spec.")
	case ".py":
		return strings.HasPrefix(b, "test_") || strings.HasPrefix(b, "tests_")
	case ".go":
		return strings.HasSuffix(b, "_test.go")
	case ".rs":
		return strings.Contains(b, "_test.rs")
	}
	return strings.HasPrefix(b, "test_")
}

// PruebaSinEscribir devuelve el primer archivo de prueba que cmd nombra
// tal cual y que no está en el repo de dir, o "". vitest y jest ignoran un
// archivo que no existe si otro de la lista sí: "run nuevo.test.ts
// viejo.test.ts" corre el viejo y sale verde, así que ese verde no dice
// nada de la prueba que falta. La ruta puede ser relativa a un `cd` o a
// un --prefix: basta con que algún archivo termine en ella. Sin git no
// opina.
func PruebaSinEscribir(dir, cmd string) string {
	c := exec.Command("git", "ls-files", "-co", "--exclude-standard")
	c.Dir = dir
	out, err := c.Output()
	if err != nil {
		return ""
	}
	archivos := strings.Split(string(out), "\n")
	for _, tkn := range strings.Fields(cmd) {
		f := strings.TrimPrefix(strings.Trim(tkn, "()\"'"), "./")
		if !EsArchivoDePrueba(f) || strings.ContainsAny(f, "*?[$") {
			continue
		}
		existe := false
		for _, a := range archivos {
			if a == f || strings.HasSuffix(a, "/"+f) {
				existe = true
				break
			}
		}
		if !existe {
			return f
		}
	}
	return ""
}

var reDjangoTest = regexp.MustCompile(`manage\.py\s+test\b`)

// SinTerminal ajusta un comando de verificación para correr sin nadie que
// conteste: loom lo ejecuta sin stdin. `manage.py test` pregunta si
// borra la base de pruebas que dejó una corrida cortada (un agente con
// timeout, una migración que falla) y muere con EOFError; con --noinput
// la borra y sigue. Lo usa todo lo que ejecuta listo_cuando o `pruebas`.
func SinTerminal(cmd string) string {
	if strings.Contains(cmd, "--noinput") || strings.Contains(cmd, "--no-input") {
		return cmd
	}
	return reDjangoTest.ReplaceAllString(cmd, "$0 --noinput")
}
