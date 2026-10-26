package topo

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/graph"
	"github.com/Pastranauwu/loom/internal/task"
)

func fixture() Entrada {
	g := &graph.Graph{Files: map[string]graph.Node{
		"internal/domain/orden.go":    {Symbols: []string{"func Nueva() Orden"}, Imports: nil},
		"internal/adapters/http.go":   {Symbols: []string{"func Handler() http.Handler"}, Imports: []string{"internal/domain"}},
		"internal/adapters/helper.go": {Imports: []string{"internal/domain"}},
		"cmd/app/main.go":             {Imports: []string{"internal/adapters"}},
	}}
	tareas := []task.Task{
		{ID: "T-001", Titulo: "dominio", TocarSolo: []string{"internal/domain/**", "internal/domain/orden_test.go"}, Expone: []string{"domain.Nueva() Orden"}},
		{ID: "T-002", Titulo: "http", TocarSolo: []string{"internal/adapters/**"}, Usa: []string{"domain.Nueva() Orden"}, Expone: []string{"POST /ordenes"}},
	}
	return Entrada{
		Grafo: g, Tareas: tareas,
		Estados: map[string]string{"T-001": "lista", "T-002": "pendiente"},
		DNA:     []dna.Arquetipo{{Nombre: "hexagonal", Reglas: "Domain logic depends on nothing."}},
	}
}

func TestConstruirModulosContratosYAristas(t *testing.T) {
	m := Construir(fixture())
	var rutas []string
	for _, mod := range m.Modulos {
		rutas = append(rutas, mod.Ruta)
	}
	if strings.Join(rutas, ",") != "internal/adapters,internal/domain" {
		t.Fatalf("con tareas solo van los módulos con tareas o contratos: %v", rutas)
	}
	dominio := m.Modulos[1]
	if len(dominio.Expone) != 1 || len(dominio.Expone[0].Consumidores) != 1 || dominio.Expone[0].Consumidores[0] != "T-002" {
		t.Errorf("el contrato de T-001 lo consume T-002: %+v", dominio.Expone)
	}
	if len(dominio.UsadoPor) != 1 || dominio.UsadoPor[0] != "internal/adapters" {
		t.Errorf("usado por = %v", dominio.UsadoPor)
	}
	adapters := m.Modulos[0]
	if len(adapters.Importa) != 1 || adapters.Importa[0] != "internal/domain" || len(adapters.Consume) != 1 {
		t.Errorf("adapters = %+v", adapters)
	}
	tipos := map[string]bool{}
	for _, a := range m.Aristas {
		tipos[a.Tipo+":"+a.De+">"+a.A] = true
	}
	if !tipos["import:internal/adapters>internal/domain"] || !tipos["uses:internal/adapters>internal/domain"] {
		t.Errorf("aristas = %+v", m.Aristas)
	}
	if m.Compuertas.Tareas["lista"] != 1 || m.Compuertas.Aceptacion != "sin correr" || !m.Compuertas.DNA || m.DNA[0].Nombre != "hexagonal" {
		t.Errorf("compuertas/dna = %+v %+v", m.Compuertas, m.DNA)
	}
}

func TestSinTareasMuestraLosModulosDelGrafo(t *testing.T) {
	e := fixture()
	e.Tareas = nil
	m := Construir(e)
	if len(m.Modulos) != 3 {
		t.Errorf("sin tareas van todos los módulos del grafo: %d", len(m.Modulos))
	}
	e.Profundidad = 1
	if m := Construir(e); len(m.Modulos) != 2 || m.Modulos[0].Ruta != "cmd" {
		t.Errorf("con profundidad 1 se agrupan por carpeta raíz: %+v", m.Modulos)
	}
}

func TestTextoYMermaid(t *testing.T) {
	m := Construir(fixture())
	txt := Texto(m, Estilo{})
	for _, quiero := range []string{"DNA", "hexagonal", "COMPUERTAS", "✓ T-001", "· T-002", "expone  domain.Nueva() Orden", "← T-002", "importa → internal/domain", "usado por ← internal/adapters"} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("el árbol no contiene %q:\n%s", quiero, txt)
		}
	}
	mm := Mermaid(m)
	for _, quiero := range []string{"flowchart LR", "%% dna: hexagonal", "--> m1_internal_domain", `-. "Nueva" .->`, "classDef lista", "class m1_internal_domain lista"} {
		if !strings.Contains(mm, quiero) {
			t.Errorf("mermaid no contiene %q:\n%s", quiero, mm)
		}
	}
	if Texto(m, Estilo{}) != txt || Mermaid(m) != mm {
		t.Error("la salida debe ser determinista")
	}
}

func TestListasLargasSeCortan(t *testing.T) {
	m := Mapa{Modulos: []Modulo{{Ruta: "hub", Importa: []string{"a", "b", "c", "d", "e", "f", "g", "h"}, NumSimbolos: 9, Simbolos: []string{"x", "y"}}}}
	txt := Texto(m, Estilo{})
	if !strings.Contains(txt, "a, b, c, d, e, f (+2)") || !strings.Contains(txt, "(+7)") {
		t.Errorf("%s", txt)
	}
}
