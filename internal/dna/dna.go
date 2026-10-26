// Package dna holds the design archetypes a project picks to steer its
// agents: named blocks of architectural rules injected next to the
// constitution. Built-ins live here; a project adds or overrides its own in
// .loom/dna/<name>.md.
package dna

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Arquetipo struct {
	Nombre string
	Reglas string
	Propio bool // from .loom/dna, not built in
}

// Resumen is the first line of the rules, for lists.
func (a Arquetipo) Resumen() string {
	l, _, _ := strings.Cut(strings.TrimSpace(strings.TrimLeft(a.Reglas, "# ")), "\n")
	return l
}

var incorporados = []Arquetipo{
	{Nombre: "crash-early", Reglas: "Fail fast and loud. Validate inputs at the boundary and return the error on the first invalid state. No silent fallbacks, no swallowed errors, no default values that hide a bug. An impossible state is an error, not a branch."},
	{Nombre: "functional-core", Reglas: "Pure functions for decisions and transformations. Side effects (IO, time, randomness, shared mutable state) live at the edges and are passed in. Prefer immutable data and small total functions."},
	{Nombre: "hexagonal", Reglas: "Domain logic depends on nothing. It talks to the outside only through ports (interfaces) it declares; adapters (HTTP, database, CLI, queues) implement them in a separate layer. Dependencies point inward: adapters, then application, then domain. No framework types inside the domain."},
	{Nombre: "zero-deps", Reglas: "Add no third-party dependency. Use the standard library and what the project already imports. If something seems to need a new package, write the few lines instead. Never edit go.mod, package.json, pyproject.toml or lockfiles to add one."},
}

// Lista returns the built-in archetypes plus the project's own, sorted by
// name; a project file replaces the built-in of the same name.
func Lista(dir string) []Arquetipo {
	por := map[string]Arquetipo{}
	for _, a := range incorporados {
		por[a.Nombre] = a
	}
	archivos, _ := filepath.Glob(filepath.Join(dir, "dna", "*.md"))
	for _, f := range archivos {
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) != "" {
			n := strings.TrimSuffix(filepath.Base(f), ".md")
			por[n] = Arquetipo{Nombre: n, Reglas: strings.TrimSpace(string(b)), Propio: true}
		}
	}
	out := make([]Arquetipo, 0, len(por))
	for _, a := range por {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nombre < out[j].Nombre })
	return out
}

// Bloque is the text injected into prompts for the chosen archetypes, in
// the order given; empty when none. Names that match no archetype come back
// in desconocidos. Same input, same bytes: it sits in the shared prompt prefix.
func Bloque(dir string, nombres []string) (bloque string, desconocidos []string) {
	if len(nombres) == 0 {
		return "", nil
	}
	todos := map[string]Arquetipo{}
	for _, a := range Lista(dir) {
		todos[a.Nombre] = a
	}
	var b strings.Builder
	for _, n := range nombres {
		a, ok := todos[n]
		if !ok {
			desconocidos = append(desconocidos, n)
			continue
		}
		if b.Len() == 0 {
			b.WriteString("Project DNA (the architectural style every agent must follow; it outranks generic habits):\n")
		}
		b.WriteString("- " + a.Nombre + ": " + strings.Join(strings.Fields(a.Reglas), " ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n"), desconocidos
}
