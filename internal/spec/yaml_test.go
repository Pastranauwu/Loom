package spec

import "testing"

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
