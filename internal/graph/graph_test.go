package graph

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repo(t *testing.T, archivos map[string]string) string {
	t.Helper()
	d := t.TempDir()
	for f, c := range archivos {
		os.MkdirAll(filepath.Dir(filepath.Join(d, f)), 0o755)
		os.WriteFile(filepath.Join(d, f), []byte(c), 0o644)
	}
	for _, a := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "x"}} {
		if out, err := exec.Command("git", append([]string{"-C", d}, a...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", a, out)
		}
	}
	return d
}

func TestGrafoIncremental(t *testing.T) {
	d := repo(t, map[string]string{
		"go.mod":      "module ejemplo.com/m\n",
		"a/a.go":      "package a\n\nfunc Suma(x, y int) int { return x + y }\n\ntype T struct{}\n",
		"b/b.go":      "package b\n\nimport \"ejemplo.com/m/a\"\n\nfunc Dos() int { return a.Suma(1, 1) }\n",
		"web/ui.ts":   "import { x } from './util'\nexport function pinta(a: string): void {}\n",
		"web/util.ts": "export const x = 1\n",
	})
	dc := filepath.Join(d, ".loom")
	g, n, err := Update(d, dc)
	if err != nil || n != 4 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if got := g.Files["a/a.go"]; len(got.Symbols) != 2 || got.Symbols[0] != "func Suma(x, y int) int" || len(got.ImportedBy) != 1 || got.ImportedBy[0] != "b/b.go" {
		t.Errorf("a.go = %+v", got)
	}
	if got := g.Files["web/util.ts"].ImportedBy; len(got) != 1 || got[0] != "web/ui.ts" {
		t.Errorf("util.ts imported_by = %v", got)
	}
	if _, n, _ = Update(d, dc); n != 0 {
		t.Errorf("sin cambios re-parseó %d", n)
	}
	os.WriteFile(filepath.Join(d, "a/a.go"), []byte("package a\n\nfunc Resta(x, y int) int { return x - y }\n"), 0o644)
	os.WriteFile(filepath.Join(d, "c.go"), []byte("package c\n\nfunc Hola() {}\n"), 0o644)
	os.Remove(filepath.Join(d, "web/util.ts"))
	g, n, _ = Update(d, dc)
	if n != 3 {
		t.Errorf("re-parseó %d, quiero 3 (a.go, c.go, util.ts borrado)", n)
	}
	if _, ok := g.Files["web/util.ts"]; ok || g.Files["a/a.go"].Symbols[0] != "func Resta(x, y int) int" {
		t.Errorf("grafo desactualizado: %+v", g.Files)
	}
}

func TestResumen(t *testing.T) {
	g := &Graph{Files: map[string]Node{
		"auth/login.go": {Symbols: []string{"func Login() error"}, ImportedBy: []string{"main.go"}},
		"util/x.go":     {Symbols: []string{"func X()"}},
	}}
	r := Resumen(g, "arregla el login")
	if !strings.Contains(r, "auth/login.go") || strings.Contains(r, "util/x.go") {
		t.Errorf("resumen = %q", r)
	}
	if Resumen(&Graph{}, "x") != "" || Resumen(g, "arregla el login") != r {
		t.Error("vacío debe ser vacío y el resumen estable")
	}
}

func TestDocDelArchivo(t *testing.T) {
	casos := map[string]string{
		"a.go": "// Package a guarda el índice. Detalle que sobra.\npackage a\n",
		"b.py": "#!/usr/bin/env python\n\"\"\"Calcula tarifas.\n\nMás texto.\"\"\"\nimport os\n",
		"c.ts": "/**\n * Cliente HTTP del backend.\n */\nexport const x = 1\n",
		"d.js": "// Rutas públicas\n// de la API.\nexport function f() {}\n",
		"e.go": "package e\n",
		"g.py": "def larga(\n    a: int,\n) -> int:\n    pass\n",
	}
	quiere := map[string]string{"a.go": "guarda el índice", "b.py": "Calcula tarifas", "c.ts": "Cliente HTTP del backend", "d.js": "Rutas públicas de la API", "e.go": "", "g.py": "", "f.py": "Cobro: único punto para cobrar; usa la pasarela"}
	for f, src := range casos {
		if doc, _, _ := parsear(f, []byte(src), ""); doc != quiere[f] {
			t.Errorf("%s: doc = %q, quiere %q", f, doc, quiere[f])
		}
	}
	if _, syms, _ := parsear("g.py", []byte(casos["g.py"]), ""); len(syms) != 1 || syms[0] != "def larga(…)" {
		t.Errorf("firma partida = %v", syms)
	}
	g := &Graph{Files: map[string]Node{"pago/cobro.go": {Doc: "cobra tarjetas", Symbols: []string{"func X()"}}}}
	if r := Resumen(g, "arregla tarjetas"); !strings.Contains(r, "pago/cobro.go (cobra tarjetas)") {
		t.Errorf("el resumen debe llevar el propósito y buscarlo: %q", r)
	}
}

func TestImportsExternosSeDescartan(t *testing.T) {
	g := &Graph{Files: map[string]Node{
		"pagos/models.py": {Imports: []string{"os", "django/conf"}},
		"pagos/vista.py":  {Imports: []string{"pagos/models", "json"}},
	}}
	enlazar(g)
	if n := g.Files["pagos/models.py"]; len(n.Imports) != 0 {
		t.Errorf("os y django son externos: %v", n.Imports)
	}
	if n := g.Files["pagos/vista.py"]; len(n.Imports) != 1 || n.Imports[0] != "pagos/models" {
		t.Errorf("imports = %v", n.Imports)
	}
}
