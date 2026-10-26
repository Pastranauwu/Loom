package ship

import (
	"strings"
	"testing"
)

func TestZeroDepsBloqueaDependenciasNuevasEnManifiestos(t *testing.T) {
	casos := []struct {
		archivo string
		lineas  []string
		bloquea bool
	}{
		{"go.mod", []string{"require github.com/spf13/cobra v1.10.2"}, true},
		{"go.mod", []string{"\tgithub.com/spf13/pflag v1.0.9 // indirect"}, true},
		{"go.mod", []string{"go 1.22", "module example.com/m", "replace example.com/a => ../a"}, false},
		{"package.json", []string{`    "left-pad": "^1.3.0",`}, true},
		{"package.json", []string{`  "version": "1.2.0",`, `  "name": "app",`, `    "test": "jest"`}, false},
		{"requirements.txt", []string{"requests>=2.31", "# comentario", "-r base.txt"}, true},
		{"requirements.txt", []string{"# solo comentario", "-r base.txt", ""}, false},
		{"pyproject.toml", []string{`  "httpx>=0.27",`}, true},
		{"pyproject.toml", []string{`name = "mi-paquete"`, `version = "0.1.0"`, `requires-python = ">=3.11"`}, false},
		{"Cargo.toml", []string{`serde = "1.0"`}, true},
		{"Cargo.toml", []string{`tokio = { version = "1", features = ["rt"] }`}, true},
		{"Cargo.toml", []string{`name = "app"`, `edition = "2021"`, `version = "0.1.0"`}, false},
		{"main.go", []string{"require github.com/x/y v1.0.0"}, false},
	}
	for _, c := range casos {
		_, ok := verificarDNA([]string{"zero-deps"}, diffDe(c.archivo, c.lineas...), nil)
		if ok == c.bloquea {
			t.Errorf("%s %q: bloquea = %v, quiero %v", c.archivo, c.lineas, !ok, c.bloquea)
		}
	}
	if _, ok := verificarDNA(nil, diffDe("go.mod", "require github.com/spf13/cobra v1.10.2"), nil); !ok {
		t.Error("sin zero-deps nada se bloquea")
	}
	if d, _ := verificarDNA([]string{"zero-deps"}, diffDe("package.json", `"left-pad": "^1.3.0"`), nil); !strings.Contains(d, "package.json") || !strings.Contains(d, "left-pad") {
		t.Errorf("el detalle debe nombrar archivo y dependencia: %q", d)
	}
}

const diffImport = `diff --git a/internal/domain/orden.go b/internal/domain/orden.go
--- a/internal/domain/orden.go
+++ b/internal/domain/orden.go
+import "example.com/m/internal/adapters/postgres"
`

func TestHexagonalPorDefectoVedaAlDominioImportarAdaptadores(t *testing.T) {
	if d, ok := verificarDNA([]string{"hexagonal"}, diffImport, nil); ok || !strings.Contains(d, "domain") {
		t.Errorf("debe bloquear: ok=%v %q", ok, d)
	}
	if _, ok := verificarDNA([]string{"functional-core"}, diffImport, nil); ok {
		t.Error("functional-core también")
	}
	// el borde sí puede importar al dominio
	bueno := strings.ReplaceAll(strings.ReplaceAll(diffImport, "internal/domain/orden.go", "internal/adapters/http.go"), "adapters/postgres", "domain")
	if _, ok := verificarDNA([]string{"hexagonal"}, bueno, nil); !ok {
		t.Error("un adaptador importando el dominio es lo correcto")
	}
	// con reglas_import propias manda lo declarado: no se aplica el default
	if _, ok := verificarDNA([]string{"hexagonal"}, diffImport, []string{"api → dominio"}); !ok {
		t.Error("con reglas_import declaradas no aplica el default")
	}
	if _, ok := verificarDNA([]string{"crash-early"}, diffImport, nil); !ok {
		t.Error("otros arquetipos no se comprueban")
	}
}
