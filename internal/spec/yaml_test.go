package spec

import (
	"os"
	"path/filepath"
	"testing"
)

// una regla o un requisito con dos puntos es prosa, no una categoría:
// YAML lo lee como mapa y antes se perdía la mitad izquierda sin avisar.
// Encontrado corriendo loom de verdad: al planificador le llegó
// "eso vive solo en la capa de terminal" por regla completa.
func TestParseNoPierdeLaMitadDeUnaReglaConDosPuntos(t *testing.T) {
	yml := `feature: snake
requirements:
  terminal:
    - la vista no decide nada: solo dibuja el snapshot
    - se controla con wasd
rules:
  - la lógica del juego no imprime ni lee el teclado: eso vive solo en la capa de terminal
`
	s, err := Parse([]byte(yml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	quiero := "la lógica del juego no imprime ni lee el teclado: eso vive solo en la capa de terminal"
	if len(s.Reglas) != 1 || s.Reglas[0] != quiero {
		t.Errorf("reglas = %q", s.Reglas)
	}
	if len(s.Requirements) != 2 {
		t.Fatalf("requirements = %q", s.Requirements)
	}
	if s.Requirements[0] != "la vista no decide nada: solo dibuja el snapshot" {
		t.Errorf("requirement con dos puntos = %q", s.Requirements[0])
	}
	// y la categoría sigue descartándose: `terminal:` no es un requisito
	for _, r := range s.Requirements {
		if r == "terminal" {
			t.Error("el nombre de la categoría llegó como requisito")
		}
	}
}

func TestParseMotivo(t *testing.T) {
	s, err := Parse([]byte("feature: exportar\nmotivo: soporte pierde 3h por semana\nrequirements:\n  - exportar a CSV\n"))
	if err != nil || s.Motivo != "soporte pierde 3h por semana" {
		t.Fatalf("motivo = %q err = %v", s.Motivo, err)
	}
	if IntencionDe(s).Igual(IntencionDe(Spec{Feature: "exportar", Requirements: []string{"exportar a CSV"}})) == false {
		t.Error("el motivo no decide qué construir: cambiarlo no debe replanear")
	}
}

func TestParseSpecEnIngles(t *testing.T) {
	s, err := Parse([]byte("title: f\nwhy: porque\nagent: dev\nmax_retries: 2\nmax_lines: 99\nconstraints:\n  dont_touch: [vendor/**]\ntasks:\n  - id: T-001\n    title: a\n    done_when: go test ./...\n    only_touch: [x/**]\n    weight: heavy\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Tasks[0]
	if s.Feature != "f" || s.Motivo != "porque" || s.Constraints.NoTocar[0] != "vendor/**" || got.Peso != "pesada" || got.Agente != "dev" || got.LimiteIntentos != 2 || got.LimiteLineas != 99 || got.ListoCuando != "go test ./..." || got.TocarSolo[0] != "x/**" {
		t.Errorf("spec = %+v", s)
	}
	if r, err := Parse(Marshal(s)); err != nil || r.Tasks[0].Peso != "pesada" || r.Limites.Intentos != 2 {
		t.Errorf("round-trip: %v %+v", err, r)
	}
}

func TestFindAceptaNombresDeDevclean(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "devclean.spec.yml"), []byte("feature: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := Find(root); err != nil || filepath.Base(p) != "devclean.spec.yml" {
		t.Fatalf("Find = %q, %v", p, err)
	}
	if err := os.WriteFile(filepath.Join(root, "loom.spec.yml"), []byte("feature: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find(root); filepath.Base(p) != "loom.spec.yml" {
		t.Errorf("loom.spec.yml debe ganar: %q", p)
	}
}
