package tui

import (
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/topo"
)

func TestVistaArquitectura(t *testing.T) {
	m := topo.Mapa{NumCapas: 2,
		Modulos:    []topo.Modulo{{Ruta: "cmd/app", Archivos: 3, Capa: 0}, {Ruta: "internal/db", Archivos: 5, Capa: 1}, {Ruta: "web/db", Archivos: 1, Capa: 1}, {Ruta: "cmd/app/tests", Archivos: 9, Capa: 0}},
		Aristas:    []topo.Arista{{De: "cmd/app", A: "internal/db"}},
		Compuertas: topo.Compuertas{Tareas: map[string]int{"lista": 4}}}
	if EnObra(m) {
		t.Fatal("todo listo no es obra")
	}
	txt := ansi.ReplaceAllString(VistaArquitectura(m, 60), "")
	for _, quiero := range []string{"app 3", "internal/db 5", "web/db 1", "app → internal/db"} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, txt)
		}
	}
	if got := lista([]string{"alfa", "beta", "gama", "delta"}, 16); got != "alfa, beta +2" {
		t.Errorf("lista = %q", got)
	}
}

func TestArquitecturaSinAndamio(t *testing.T) {
	m := topo.Mapa{NumCapas: 1, Modulos: []topo.Modulo{{Ruta: "core/tests"}, {Ruta: "core/migrations"}, {Ruta: "."}}}
	txt := ansi.ReplaceAllString(VistaArquitectura(m, 60), "")
	if strings.Contains(txt, "tests") || strings.Contains(txt, "migrations") || !strings.Contains(txt, "raíz") {
		t.Fatalf("andamio en el bosquejo:\n%s", txt)
	}
}
