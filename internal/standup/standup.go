// Package standup implements the deterministic data standup report.
// All events are derived from attempts.jsonl; no model is used for the
// deterministic detectors. Inputs come from the caller to keep this
// package pure.
package standup

import (
	"fmt"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// TipoEvento classifies each standup alert.
type TipoEvento int

const (
	EventoColision     TipoEvento = iota // shared exported symbols between active tasks
	EventoAtasco                         // no test-count progress for > UmbralAtasco
	EventoInterrumpida                   // la corrida murió encima de la tarea
	EventoOK                             // within scope, no issues
)

// Evento is one standup line.
type Evento struct {
	Tipo    TipoEvento `json:"tipo"`
	TareaID string     `json:"tarea_id"`
	Detalle string     `json:"detalle"`
}

// detectarFaseLenta marca una tarea cuya fase actual lleva más de
// UmbralAtasco sin cambiar. Es lo único que ve una invocación colgada:
// mientras el agente no vuelve, no hay intento que registrar.
func detectarFaseLenta(l loop.Latido) (Evento, bool) {
	if l.ID == "" || !l.Vivo() {
		return Evento{}, false // sin latido, o con la corrida muerta encima
	}
	transcurrido := l.EnFaseDesde()
	if transcurrido < UmbralAtasco {
		return Evento{}, false
	}
	return Evento{
		Tipo:    EventoAtasco,
		TareaID: l.ID,
		Detalle: fmt.Sprintf("%s lleva %s en %s (intento %d) sin dar señal · mira loom logs %s",
			l.ID, redondear(transcurrido), l.Fase, l.Intento, l.ID),
	}, true
}

// redondear deja la duración en minutos enteros, que es la unidad en la
// que uno decide si algo está colgado.
func redondear(d time.Duration) string {
	return d.Round(time.Minute).String()
}

// UmbralAtasco is the maximum duration without test-count improvement
// before flagging a task as stuck.
const UmbralAtasco = 10 * time.Minute

// Analizar derives all standup events from disk state. No model is used.
//
// latidos son los latidos EN CRUDO de las tareas (internal/loop), vivos
// o no. Sin ellos el parte solo veía intentos ya terminados: una tarea
// llevaba cuarenta minutos colgada en una sola invocación y el informe
// decía "dentro de contrato", porque attempts.jsonl todavía no tenía
// nada que contar. Puede venir nil (comandos que no los cargan).
//
// Tienen que venir en crudo, no filtrados por LeerLatidos: la diferencia
// entre un latido fresco y uno rancio es la que separa "el agente sigue
// trabajando y se colgó" (ATASCO) de "el proceso que la trabajaba ya no
// existe" (MUERTA). Filtrados, las dos se ven igual que un hueco.
func Analizar(
	tareas []task.Task,
	estados map[string]state.State,
	attempts map[string][]loop.Attempt,
	latidos map[string]loop.Latido,
) []Evento {
	activas := activasCon(tareas, estados)
	var eventos []Evento

	// 1. colisión: shared exported symbols between in-progress tasks
	eventos = append(eventos, detectarColisiones(activas, attempts)...)

	// 2. atasco en vivo: la fase actual lleva demasiado sin moverse
	for _, t := range activas {
		if e, ok := detectarFaseLenta(latidos[t.ID]); ok {
			eventos = append(eventos, e)
		}
	}

	// 3. corrida muerta encima: hay latido pero nadie lo refresca. Va
	// antes que el atasco porque explica el silencio: la tarea no está
	// colgada, el proceso que la trabajaba ya no existe.
	for _, t := range activas {
		if l, ok := latidos[t.ID]; ok && !l.Vivo() {
			eventos = append(eventos, Evento{
				Tipo:    EventoInterrumpida,
				TareaID: t.ID,
				Detalle: fmt.Sprintf("%s quedó en curso pero su corrida murió hace %s (intento %d, fase %s) · retómala con loom run --reintentar",
					t.ID, redondear(l.Silencio()), l.Intento, l.Fase),
			})
		}
	}

	// 4. atasco entre intentos: no test progress for > UmbralAtasco
	for _, t := range activas {
		if l, corriendo := latidos[t.ID]; corriendo && l.Vivo() {
			continue // ya se juzgó por su fase en vivo
		}
		if e, ok := detectarAtasco(t.ID, attempts[t.ID]); ok {
			eventos = append(eventos, e)
		}
	}

	// ok: active tasks with no alerts
	alertadas := map[string]bool{}
	for _, e := range eventos {
		alertadas[e.TareaID] = true
	}
	for _, t := range activas {
		if !alertadas[t.ID] {
			eventos = append(eventos, Evento{Tipo: EventoOK, TareaID: t.ID})
		}
	}
	return eventos
}

// Formatear renders the standup report in its canonical format.
func Formatear(eventos []Evento, ahora time.Time, nActivas int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PARTE %s · %d tareas en curso\n", ahora.Format("15:04"), nActivas)
	if len(eventos) == 0 {
		b.WriteString("\n✓ sin alertas")
		return b.String()
	}
	b.WriteString("\n")
	for _, e := range eventos {
		switch e.Tipo {
		case EventoColision:
			fmt.Fprintf(&b, "⚠ COLISIÓN   %s\n", e.Detalle)
		case EventoAtasco:
			fmt.Fprintf(&b, "⚠ ATASCO     %s\n", e.Detalle)
		case EventoInterrumpida:
			fmt.Fprintf(&b, "⚠ MUERTA     %s\n", e.Detalle)
		case EventoOK:
			fmt.Fprintf(&b, "✓            %s dentro de contrato\n", e.TareaID)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func activasCon(tareas []task.Task, estados map[string]state.State) []task.Task {
	var out []task.Task
	for _, t := range tareas {
		if s, ok := estados[t.ID]; ok && s.Estado == state.EnCurso {
			out = append(out, t)
		}
	}
	return out
}

func detectarColisiones(activas []task.Task, attempts map[string][]loop.Attempt) []Evento {
	ultimosSimbolos := make(map[string][]string, len(activas))
	for _, t := range activas {
		as := attempts[t.ID]
		if len(as) == 0 {
			continue
		}
		if s := as[len(as)-1].SimbolosExportados; s != nil {
			ultimosSimbolos[t.ID] = *s
		}
	}

	var eventos []Evento
	reportadas := map[string]bool{}
	for i, a := range activas {
		for _, b := range activas[i+1:] {
			clave := a.ID + "+" + b.ID
			if reportadas[clave] {
				continue
			}
			comunes := interseccion(ultimosSimbolos[a.ID], ultimosSimbolos[b.ID])
			if len(comunes) == 0 {
				continue
			}
			reportadas[clave] = true
			detalle := fmt.Sprintf("%s y %s modifican símbolo(s) exportado(s) en común: %s", a.ID, b.ID, strings.Join(comunes, ", "))
			eventos = append(eventos, Evento{Tipo: EventoColision, TareaID: a.ID, Detalle: detalle})
		}
	}
	return eventos
}

func detectarAtasco(id string, as []loop.Attempt) (Evento, bool) {
	if len(as) < 2 {
		return Evento{}, false
	}
	ultimo := as[len(as)-1]
	if time.Since(ultimo.Fin) < UmbralAtasco {
		return Evento{}, false
	}
	// find earliest attempt still within the window
	var referencia *loop.Attempt
	for i := range as {
		if time.Since(as[i].Fin) <= UmbralAtasco {
			referencia = &as[i]
			break
		}
	}
	if referencia == nil {
		referencia = &as[0]
	}
	if ultimo.TestsPasaron == nil || referencia.TestsPasaron == nil {
		return Evento{}, false
	}
	if *ultimo.TestsPasaron > *referencia.TestsPasaron {
		return Evento{}, false // progreso: pasaron más tests
	}
	mins := int(time.Since(ultimo.Fin).Minutes())
	detalle := fmt.Sprintf("%s sin cambio en el conteo de tests desde hace %d min", id, mins)
	return Evento{Tipo: EventoAtasco, TareaID: id, Detalle: detalle}, true
}

func interseccion(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range b {
		if set[s] && !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}
