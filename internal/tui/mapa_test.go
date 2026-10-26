package tui

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/graph"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/topo"
)

func mapaDePrueba() topo.Mapa {
	return topo.Construir(topo.Entrada{
		Grafo: &graph.Graph{Files: map[string]graph.Node{
			"cmd/app/main.go":           {Imports: []string{"internal/adapters"}},
			"internal/adapters/http.go": {Symbols: []string{"func Handler() http.Handler"}, Imports: []string{"internal/domain"}},
			"internal/domain/orden.go":  {Symbols: []string{"func Nueva() Orden"}},
		}},
		Tareas:  []task.Task{{ID: "T-001", Titulo: "dominio", TocarSolo: []string{"internal/domain/**"}, Expone: []string{"domain.Nueva() Orden"}}},
		Estados: map[string]string{"T-001": "lista"},
		DNA:     []dna.Arquetipo{{Nombre: "hexagonal", Reglas: "x"}},
		Todos:   true,
	})
}

func TestVistaMapaEnCapasEnIngles(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "en")
	idioma.Set("en")
	v := VistaMapa(mapaDePrueba(), 100)
	for _, quiero := range []string{"loom map", "◈ hexagonal", "✓ 1 ready", "acceptance not run", "Entry points", "Foundations", "depends on", "cmd/app", "internal/domain", "✓ T-001 dominio", "◆ domain.Nueva() Orden", "→ domain", "╭"} {
		if !strings.Contains(v, quiero) {
			t.Errorf("la vista no contiene %q:\n%s", quiero, v)
		}
	}
	if strings.Index(v, "cmd/app") > strings.Index(v, "internal/domain") {
		t.Error("las entradas van arriba y los cimientos abajo")
	}
}

func TestVistaMapaEnEspanol(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "es")
	idioma.Set("es")
	defer idioma.Set("en")
	v := VistaMapa(mapaDePrueba(), 100)
	for _, quiero := range []string{"Puntos de entrada", "Cimientos", "1 listas", "aceptación sin correr"} {
		if !strings.Contains(v, quiero) {
			t.Errorf("sin %q:\n%s", quiero, v)
		}
	}
}

func TestVistaMapaEstrechaAcomodaTarjetasEnFilas(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "en")
	idioma.Set("en")
	for _, l := range strings.Split(VistaMapa(mapaDePrueba(), 50), "\n") {
		if n := len([]rune(stripANSI(l))); n > 50 {
			t.Errorf("línea de %d columnas en una terminal de 50: %q", n, l)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestTableroYEsclusaEnIngles(t *testing.T) {
	t.Setenv("LOOM_LANGUAGE", "en")
	idioma.Set("en")
	defer idioma.Set("en")
	g := renderGate("T-001", nil, false, 0, 100, 0)
	if !strings.Contains(g, "EXIT GATE · T-001") || !strings.Contains(g, "noise") {
		t.Errorf("esclusa en inglés:\n%s", g)
	}
}
