// Package constitution manages .loom/constitution.md:
// project-wide conventions injected into every agent context so that
// parallel agents make compatible design decisions.
package constitution

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const FileName = "constitution.md"

// Path returns the constitution.md path for the repository at root.
func Path(root string) string {
	return filepath.Join(root, ".loom", FileName)
}

// Exists reports whether the constitution file is present.
func Exists(root string) bool {
	_, err := os.Stat(Path(root))
	return err == nil
}

// Load reads the constitution. Returns "" when absent — not an error.
func Load(root string) (string, error) {
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(data), err
}

// Save writes content to .loom/constitution.md.
func Save(root, content string) error {
	return os.WriteFile(Path(root), []byte(content), 0o644)
}

// Prompt builds the generation instruction for the constitution model.
// lenguaje and pruebas come from config; arbol is git ls-files output (truncated).
func Prompt(lenguaje, pruebas, arbol string) string {
	var b strings.Builder
	b.WriteString("Eres el arquitecto de loom. Genera una constitución de proyecto corta y concreta.\n\n")
	b.WriteString("La constitución es un archivo markdown que todos los agentes recibirán como contexto antes de programar. Propósito: evitar que agentes en paralelo tomen decisiones de diseño incompatibles.\n\n")
	if lenguaje != "" {
		b.WriteString("Lenguaje del proyecto: " + lenguaje + "\n")
	}
	if pruebas != "" {
		b.WriteString("Comando de pruebas: " + pruebas + "\n")
	}
	if arbol != "" {
		b.WriteString("Estructura del proyecto:\n```\n" + arbol + "\n```\n\n")
	}
	b.WriteString(`Genera un documento markdown con estas secciones EXACTAS, sin añadir más:

# Constitución del proyecto

## Estructura de capas
[Arquitectura en capas: qué paquete puede importar a cuál. Máximo 3 líneas.]

## Convenciones de estilo
[Convenciones del lenguaje. Nombres, formato, errores. Máximo 5 puntos concretos.]

## Patrones prohibidos
[Qué NO hacer. Máximo 3 ítems concretos y verificables.]

## Tamaño máximo por archivo
[Máximo de líneas, cuándo partir. Máximo 2 líneas.]

Reglas:
- Concreto y verificable. Nada de platitudes.
- Máximo 300 palabras en total.
- No añadas secciones extra.
- Devuelve SOLO el markdown, sin texto alrededor.`)
	return b.String()
}
