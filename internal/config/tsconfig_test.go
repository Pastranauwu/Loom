package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProblemasTsconfig(t *testing.T) {
	jsonc := `{
  // comentario
  "compilerOptions": {
    "moduleResolution": "Node", /* bloque */
    "target": "es3",
    "importsNotUsedAsValues": "remove",
    "paths": { "@/*": ["./src/*"] },
    "strict": true,
  },
}`
	got := strings.Join(ProblemasTsconfig([]byte(jsonc)), "\n")
	for _, quiero := range []string{`moduleResolution "Node"`, `target "ES3"`, "importsNotUsedAsValues"} {
		if !strings.Contains(got, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, got)
		}
	}
	if p := ProblemasTsconfig([]byte(`{"compilerOptions":{"moduleResolution":"bundler","strict":true}}`)); len(p) != 0 {
		t.Errorf("moderno no tiene problemas: %v", p)
	}
}

func TestTsconfigObsoletoRecorreElProyecto(t *testing.T) {
	dir := t.TempDir()
	for ruta, c := range map[string]string{
		"tsconfig.json":                  `{"compilerOptions":{"moduleResolution":"node10"}}`,
		"packages/web/tsconfig.app.json": `{"compilerOptions":{"moduleResolution":"bundler"}}`,
		"node_modules/x/tsconfig.json":   `{"compilerOptions":{"moduleResolution":"node"}}`,
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, ruta)), 0o755)
		os.WriteFile(filepath.Join(dir, ruta), []byte(c), 0o644)
	}
	p := TsconfigObsoleto(dir)
	if len(p) != 1 || !strings.HasPrefix(p[0], "tsconfig.json: ") {
		t.Errorf("problemas = %v · node_modules no cuenta", p)
	}
}

func TestSandboxEnConfig(t *testing.T) {
	cfg, err := Parse([]byte("base: main\nsandbox: off\n"))
	if err != nil || cfg.Sandbox != "off" {
		t.Fatalf("sandbox: off = %q, %v", cfg.Sandbox, err)
	}
	if _, err := Parse([]byte("base: main\nsandbox: quizas\n")); err == nil {
		t.Error("un valor desconocido es error")
	}
	dir := t.TempDir()
	os.MkdirAll(Dir(dir), 0o755)
	if err := (Config{Base: "main", Sandbox: "off"}).Save(dir); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(dir))
	if !strings.Contains(string(b), "sandbox: off") {
		t.Errorf("se escribe en inglés:\n%s", b)
	}
}
