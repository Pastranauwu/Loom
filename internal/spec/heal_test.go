package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agregar(t *testing.T, yml string) (string, Spec) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "loom.spec.yml")
	if err := os.WriteFile(p, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AgregarReglaYCriterio(p, "nunca ids vacíos", Acceptance{Criterion: "id vacío rechazado", Command: "go test ./... -run TestVacio"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s, err := Parse(b)
	if err != nil {
		t.Fatalf("el resultado no parsea: %v\n%s", err, b)
	}
	return string(b), s
}

func TestAgregarConservaComentariosYAgrega(t *testing.T) {
	txt, s := agregar(t, "# mi comentario\nfeature: tienda\nrequirements:\n  - vender\nrules:\n  - sin sesiones\nacceptance:\n  - flujo completo\n")
	if !strings.Contains(txt, "# mi comentario") {
		t.Errorf("perdió el comentario:\n%s", txt)
	}
	if len(s.Reglas) != 2 || s.Reglas[1] != "nunca ids vacíos" || len(s.Acceptance) != 2 || s.Acceptance[1].Command != "go test ./... -run TestVacio" || len(s.Requirements) != 1 {
		t.Errorf("spec = %+v", s)
	}
}

func TestAgregarCreaLasSeccionesQueFaltan(t *testing.T) {
	_, s := agregar(t, "feature: tienda\nrequirements:\n  - vender\n")
	if len(s.Reglas) != 1 || len(s.Acceptance) != 1 {
		t.Errorf("spec = %+v", s)
	}
}

func TestAgregarEnReglasConCategorias(t *testing.T) {
	_, s := agregar(t, "feature: tienda\nrequirements:\n  - vender\nrules:\n  style:\n    - sin deps\nacceptance:\n  - a\n")
	if len(s.Reglas) != 2 {
		t.Errorf("reglas = %v", s.Reglas)
	}
}

func TestAgregarNoEscribeSiNoParsea(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.yml")
	os.WriteFile(p, []byte("campo_raro: 1\n"), 0o644)
	if err := AgregarReglaYCriterio(p, "x", Acceptance{Criterion: "y"}); err == nil {
		t.Fatal("un spec inválido debe fallar")
	}
	if b, _ := os.ReadFile(p); string(b) != "campo_raro: 1\n" {
		t.Errorf("el archivo cambió: %q", b)
	}
}
