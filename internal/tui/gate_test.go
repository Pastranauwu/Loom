package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Pastranauwu/loom/internal/ship"
)

func TestClasificar(t *testing.T) {
	pasos := []ship.Paso{
		{Nombre: "base", OK: true},
		{Nombre: "historial", OK: true},
		{Nombre: "ruido", OK: false},
	}
	e := clasificar(pasos, false)
	if e[0] != verde || e[1] != verde || e[2] != rojo {
		t.Errorf("estados = %v, quiero verde, verde, rojo", e)
	}
	if e[3] != trabajando {
		t.Errorf("el paso siguiente debió quedar trabajando, es %v", e[3])
	}
	if e[len(e)-1] != pendiente {
		t.Errorf("el último paso debió quedar pendiente, es %v", e[len(e)-1])
	}
}

func TestClasificarTerminado(t *testing.T) {
	e := clasificar([]ship.Paso{{Nombre: "base", OK: true}}, true)
	for i, s := range e {
		if s == trabajando {
			t.Errorf("terminado no debió dejar nada trabajando (paso %d)", i)
		}
	}
}

func TestRenderGate(t *testing.T) {
	pasos := []ship.Paso{
		{Nombre: "base", OK: true},
		{Nombre: "historial", OK: true, Detalle: "47 guardados → 1 commit"},
	}
	vista := renderGate("T-003", pasos, false, 0, 0, 0)

	for _, want := range []string{"ESCLUSA DE SALIDA · T-003", "base", "hist", "ruido", "secr", "presu", "iface", "bisec", "hand", "pr", "✓", "·", "2/11"} {
		if !strings.Contains(vista, want) {
			t.Errorf("la vista no contiene %q:\n%s", want, vista)
		}
	}
	if !strings.Contains(vista, "47 guardados → 1 commit") {
		t.Errorf("falta el detalle del último paso:\n%s", vista)
	}
}

func TestRenderGateFreno(t *testing.T) {
	pasos := []ship.Paso{{Nombre: "base", OK: false, Detalle: "rebase en conflicto"}}
	vista := renderGate("T-003", pasos, true, 0, 0, 0)
	if !strings.Contains(vista, "✗") {
		t.Errorf("un paso fallado debió pintarse con ✗:\n%s", vista)
	}
	if !strings.Contains(vista, "rebase en conflicto") {
		t.Errorf("falta el motivo del freno:\n%s", vista)
	}
}

func TestLogo(t *testing.T) {
	l := Logo(80)
	for _, want := range []string{"▄", "▀", "█", "dirige agentes"} {
		if !strings.Contains(l, want) {
			t.Errorf("logo sin %q:\n%s", want, l)
		}
	}
}

func TestBarra(t *testing.T) {
	if !strings.Contains(barra(4, 8, 10), "████") {
		t.Error("la barra a medias debió llenar la mitad")
	}
	if !strings.Contains(barra(0, 8, 10), "░░") {
		t.Error("la barra vacía debió quedar en gris")
	}
}

// Las dos listas se leen en paralelo: si una crece y la otra no, las
// etiquetas se corren y nadie se entera hasta ver un GIF publicado.
func TestEtiquetasAlineadas(t *testing.T) {
	if len(NombresPasos) != len(nombresCortos) {
		t.Fatalf("NombresPasos tiene %d y nombresCortos %d", len(NombresPasos), len(nombresCortos))
	}
}

// El bug que se vio en el GIF: la compuerta emitió 9 pasos contra 8
// etiquetas y el contador marcó 9/8.
func TestContadorNuncaPasaDelTotal(t *testing.T) {
	var pasos []ship.Paso
	for _, n := range NombresPasos {
		pasos = append(pasos, ship.Paso{Nombre: n, OK: true})
	}
	// más pasos de los que la fila conoce: los del camino multitarea
	pasos = append(pasos, ship.Paso{Nombre: "esclusa T-001", OK: true})
	pasos = append(pasos, ship.Paso{Nombre: "integradas", OK: true})

	vista := renderGate("T-001", pasos, true, 0, 100, 0)
	if !strings.Contains(vista, "11/11") {
		t.Errorf("contador fuera de rango, quiero 11/11:\n%s", vista)
	}
	for _, malo := range []string{"12/11", "13/11", "/8"} {
		if strings.Contains(vista, malo) {
			t.Errorf("el contador dice %q:\n%s", malo, vista)
		}
	}
}

// Un paso que no aplica (sin suite sellada) no debe leerse como si
// faltara trabajo: la corrida cierra en hechos/hechos.
func TestPasoQueNoAplicaNoDejaLaBarraCorta(t *testing.T) {
	var pasos []ship.Paso
	for _, n := range NombresPasos {
		if n == "suite_oculta" || n == "dependencias" {
			continue // esta corrida no los ejecutó
		}
		pasos = append(pasos, ship.Paso{Nombre: n, OK: true})
	}
	vista := renderGate("T-001", pasos, true, 0, 100, 0)
	if !strings.Contains(vista, "9/9") {
		t.Errorf("quiero 9/9 con dos pasos que no aplican:\n%s", vista)
	}
}

// Un salto en medio no debe correr las etiquetas siguientes: el mapeo es
// por nombre, no por posición.
func TestSaltoNoCorreLasEtiquetas(t *testing.T) {
	// llega "pr" (el último) sin los intermedios
	e := clasificar([]ship.Paso{
		{Nombre: "base", OK: true},
		{Nombre: "pr", OK: true},
	}, true)
	if e[0] != verde {
		t.Errorf("base debió quedar verde, es %v", e[0])
	}
	if e[len(e)-1] != verde {
		t.Errorf("pr debió quedar verde en su posición, es %v", e[len(e)-1])
	}
	if e[1] != pendiente {
		t.Errorf("historial no corrió: debió quedar pendiente, es %v", e[1])
	}
}

func TestRenderGateCabeEnTerminalChica(t *testing.T) {
	pasos := []ship.Paso{{Nombre: "base", OK: true, Detalle: strings.Repeat("detalle largo ", 30)}}
	vista := renderGate("T-001", pasos, false, 0, 60, 24)
	lineas := strings.Split(vista, "\n")
	if len(lineas) > 24 {
		t.Errorf("%d líneas en una terminal de 24", len(lineas))
	}
	for _, l := range lineas {
		if w := lipgloss.Width(l); w > 60 {
			t.Errorf("línea de %d columnas en una terminal de 60: %q", w, l)
		}
	}
	if !strings.Contains(vista, "bisectable") {
		t.Error("en angosto los pasos van en lista con su nombre completo")
	}
}
