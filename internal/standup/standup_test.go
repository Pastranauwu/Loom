package standup

import (
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func activa(id string) ([]task.Task, map[string]state.State) {
	return []task.Task{{ID: id, Titulo: "t"}},
		map[string]state.State{id: {ID: id, Estado: state.EnCurso}}
}

// vivo marca el latido como refrescado ahora mismo, que es lo que hace
// una corrida en pie. Sin esto el fixture describe una corrida muerta:
// `Visto` en cero es un latido que nadie refresca.
func vivo(l loop.Latido) loop.Latido {
	l.Visto = time.Now()
	return l
}

// El caso que motivó el latido: una invocación lleva 40 minutos sin
// volver. attempts.jsonl está vacío, así que el parte informaba "dentro
// de contrato" de una tarea colgada.
func TestAnalizarDetectaFaseColgada(t *testing.T) {
	tareas, estados := activa("T-003")
	latidos := map[string]loop.Latido{
		"T-003": vivo(loop.Latido{
			ID: "T-003", Intento: 1, Limite: 3, Fase: loop.FaseAgente,
			Modelo: "opencode/big-pickle", DesdeFase: time.Now().Add(-40 * time.Minute),
		}),
	}

	eventos := Analizar(tareas, estados, nil, latidos)
	if len(eventos) != 1 {
		t.Fatalf("eventos = %+v, quiero 1", eventos)
	}
	if eventos[0].Tipo != EventoAtasco {
		t.Fatalf("tipo = %v, quiero EventoAtasco", eventos[0].Tipo)
	}
	for _, quiero := range []string{"T-003", "40m", loop.FaseAgente} {
		if !strings.Contains(eventos[0].Detalle, quiero) {
			t.Errorf("detalle %q no menciona %q", eventos[0].Detalle, quiero)
		}
	}
}

// Una fase recién arrancada no es un atasco.
func TestAnalizarNoAlarmaFaseJoven(t *testing.T) {
	tareas, estados := activa("T-001")
	latidos := map[string]loop.Latido{
		"T-001": vivo(loop.Latido{ID: "T-001", Intento: 1, Fase: loop.FaseAgente, DesdeFase: time.Now().Add(-1 * time.Minute)}),
	}

	eventos := Analizar(tareas, estados, nil, latidos)
	if len(eventos) != 1 || eventos[0].Tipo != EventoOK {
		t.Errorf("eventos = %+v, quiero un solo EventoOK", eventos)
	}
}

// Sin latidos el parte sigue funcionando como antes: los comandos que no
// los cargan pasan nil.
func TestAnalizarSinLatidos(t *testing.T) {
	tareas, estados := activa("T-001")
	eventos := Analizar(tareas, estados, nil, nil)
	if len(eventos) != 1 || eventos[0].Tipo != EventoOK {
		t.Errorf("eventos = %+v", eventos)
	}
}

// Una fase vieja con latido fresco es un ATASCO (el agente sigue vivo y
// colgado); la misma fase con latido rancio es una corrida MUERTA. Es la
// distinción que justifica el campo Visto: sin él las dos se veían igual.
func TestAnalizarDistingueAtascoDeCorridaMuerta(t *testing.T) {
	tareas, estados := activa("T-001")
	base := loop.Latido{
		ID: "T-001", Intento: 2, Limite: 3, Fase: loop.FaseAgente,
		DesdeFase: time.Now().Add(-40 * time.Minute),
	}

	colgada := base
	colgada.Visto = time.Now()
	ev := Analizar(tareas, estados, nil, map[string]loop.Latido{"T-001": colgada})
	if len(ev) != 1 || ev[0].Tipo != EventoAtasco {
		t.Fatalf("latido fresco: eventos = %+v, quiero EventoAtasco", ev)
	}

	muerta := base
	muerta.Visto = time.Now().Add(-loop.LatidoRancioTras - time.Minute)
	ev = Analizar(tareas, estados, nil, map[string]loop.Latido{"T-001": muerta})
	if len(ev) != 1 || ev[0].Tipo != EventoInterrumpida {
		t.Fatalf("latido rancio: eventos = %+v, quiero EventoInterrumpida", ev)
	}
	if !strings.Contains(ev[0].Detalle, "--reintentar") {
		t.Errorf("el parte no dice cómo retomarla: %q", ev[0].Detalle)
	}
}
