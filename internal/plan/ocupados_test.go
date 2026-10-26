package plan

import (
	"strings"
	"testing"
)

// El hallazgo: el planificador no sabía qué alcances ya tienen dueño, así
// que proponía tareas que la esclusa de entrada rechazaba después, con
// los tokens del plan ya gastados.
func TestPromptDeclaraAlcancesOcupados(t *testing.T) {
	c := Contexto{
		Lenguaje: "go",
		Ocupados: map[string][]string{
			"T-001": {"src/export/**"},
			"T-002": {"docs/**", "internal/api/**"},
		},
	}
	p := Prompt("exportar clientes", c)
	for _, quiero := range []string{"T-001", "src/export/**", "T-002", "internal/api/**"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("el prompt no menciona %q", quiero)
		}
	}
	if !strings.Contains(p, "running task") {
		t.Error("el prompt no explica por qué esas rutas están vedadas")
	}
}

// Sin tareas activas el prompt no debe cargar con una sección vacía.
func TestPromptSinOcupadosNoDiceNada(t *testing.T) {
	p := Prompt("algo", Contexto{Lenguaje: "go"})
	if strings.Contains(p, "ALREADY being touched") || strings.Contains(p, "running task;") {
		t.Error("sección de ocupados con el mapa vacío")
	}
}

// El prompt tiene que ser byte a byte igual entre corridas: si el orden
// del map se filtra, la caché del proveedor deja de acertar.
func TestPromptOcupadosEsEstable(t *testing.T) {
	c := Contexto{Ocupados: map[string][]string{
		"T-003": {"c/**"}, "T-001": {"a/**"}, "T-002": {"b/**"},
	}}
	primero := Prompt("x", c)
	for i := 0; i < 20; i++ {
		if Prompt("x", c) != primero {
			t.Fatal("el prompt cambia entre llamadas: el orden del map se filtró")
		}
	}
	ia := strings.Index(primero, "T-001")
	ib := strings.Index(primero, "T-002")
	ic := strings.Index(primero, "T-003")
	if !(ia < ib && ib < ic) {
		t.Errorf("los ids no salen ordenados: %d %d %d", ia, ib, ic)
	}
}
