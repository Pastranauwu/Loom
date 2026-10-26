// Package constitution manages .loom/constitution.md:
// project-wide conventions injected into every agent context so that
// parallel agents make compatible design decisions.
package constitution

import (
	"errors"
	"github.com/Pastranauwu/loom/internal/idioma"
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
	b.WriteString("You are loom's architect. Generate a short, concrete project constitution.\n\n")
	b.WriteString("The constitution is a markdown file every agent will receive as context before coding. Purpose: keep parallel agents from making incompatible design decisions.\n\n")
	if lenguaje != "" {
		b.WriteString("Project language: " + lenguaje + "\n")
	}
	if pruebas != "" {
		b.WriteString("Test command: " + pruebas + "\n")
	}
	if arbol != "" {
		b.WriteString("Project structure:\n```\n" + arbol + "\n```\n\n")
	}
	b.WriteString(`Generate a markdown document with these EXACT sections, adding no more:

# Project constitution

## Layer structure
[Layered architecture: which package may import which. At most 3 lines.]

## Style conventions
[Language conventions. Names, formatting, errors. At most 5 concrete points.]

## Forbidden patterns
[What NOT to do. At most 3 concrete, verifiable items. Always include: agents, not people, maintain the code; no comments for humans, no commented-out code and no wrappers just for readability.]

## Maximum file size
[Maximum lines, when to split. At most 2 lines.]

Rules:
- Concrete and verifiable. No platitudes.
- At most 300 words in total.
- Do not add extra sections.
- Return ONLY the markdown, no text around it.`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}
