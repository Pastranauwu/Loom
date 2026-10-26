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
