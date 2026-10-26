package loop

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"path/filepath"
	"time"
)

// Fases de un intento, en el orden en que ocurren.
const (
	FaseExamen      = "examen"      // el examinador escribe la suite ciega
	FaseAgente      = "agente"      // el implementador trabaja
	FaseVerificando = "verificando" // corre listo_cuando
	FaseRevision    = "revision"    // el revisor juzga el diff verde contra el contrato
)

// Cada cuánto late una corrida viva, y a partir de cuándo un latido sin
// refrescar se da por muerto.
//
// La proporción es deliberadamente holgada — seis intervalos, no los dos
// o tres de la regla común. Declarar muerta una corrida viva es el error
// caro: `run --reintentar` reviviría una tarea que otro proceso está
// trabajando, y dos agentes en el mismo cuarto se pisan. Al revés solo
// se espera minuto y medio. Escribir el latido es un WriteFile chico a
// disco local, así que latir seguido no cuesta nada.
const (
	LatidoIntervalo  = 15 * time.Second
	LatidoRancioTras = 90 * time.Second
)

// Latido es el estado vivo de una tarea: en qué fase está ahora mismo,
// desde cuándo, y cuándo fue la última señal de vida de su corrida.
//
// Existe porque attempts.jsonl solo se escribe cuando un intento
// TERMINA. Durante los veinte minutos que puede durar una invocación no
// había un solo byte en disco, así que `standup` informaba "dentro de
// contrato" de una tarea colgada y el tablero no tenía nada que pintar.
//
// El archivo se borra al terminar la tarea, pero eso solo cubre la
// salida ordenada: un SIGKILL (la laptop suspende, se cae el ssh) deja
// el latido en disco para siempre, y con él el tablero decía "en curso"
// y el standup "dentro de contrato" de una tarea muerta. Por eso la
// pregunta no es si el archivo existe sino si `Visto` es reciente: una
// corrida viva lo refresca cada LatidoIntervalo.
type Latido struct {
	ID        string    `json:"id"`
	Intento   int       `json:"intento"`
	Limite    int       `json:"limite"`
	Fase      string    `json:"fase"`
	Modelo    string    `json:"modelo"`
	DesdeFase time.Time `json:"desde_fase"`
	Tokens    Tokens    `json:"tokens"` // acumulado de la tarea hasta ahora
	// Visto es la última señal de vida. Cero en un latido viejo escrito
	// por un binario anterior: se trata como rancio, que es lo seguro.
	Visto time.Time `json:"visto"`
}

// EnFaseDesde devuelve cuánto lleva la tarea en su fase actual.
func (l Latido) EnFaseDesde() time.Duration { return time.Since(l.DesdeFase) }

// Vivo dice si la corrida que escribió este latido sigue en pie.
func (l Latido) Vivo() bool { return time.Since(l.Visto) < LatidoRancioTras }

// Silencio devuelve cuánto lleva el latido sin refrescarse.
func (l Latido) Silencio() time.Duration { return time.Since(l.Visto) }

// Descripcion resume el latido en una línea para el tablero.
func (l Latido) Descripcion() string {
	s := fmt.Sprintf(idioma.T("intento %d"), l.Intento)
	if l.Limite > 0 {
		s += fmt.Sprintf("/%d", l.Limite)
	}
	s += " · " + idioma.T(l.Fase)
	if l.Modelo != "" {
		s += " · " + l.Modelo
	}
	return s
}

// latidoPath devuelve la ruta del latido de una tarea.
func latidoPath(root, id string) string {
	return filepath.Join(RunsDir(root), id, "latido.json")
}

// EscribirLatido guarda el estado vivo de una tarea y la marca vista
// ahora. Falla en silencio: perder el latido nunca debe frenar el
// trabajo real.
func EscribirLatido(root string, l Latido) {
	l.Visto = time.Now().UTC()
	p := latidoPath(root, l.ID)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	datos, err := json.Marshal(l)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, datos, 0o644)
}

// BorrarLatido quita el latido de una tarea que dejó de correr.
func BorrarLatido(root, id string) {
	_ = os.Remove(latidoPath(root, id))
}

// LeerLatido devuelve el latido de una tarea solo si su corrida sigue
// viva. Un latido rancio — el que deja un SIGKILL — cuenta como que no
// hay nadie trabajando; para verlo igual, LeerLatidoCrudo.
func LeerLatido(root, id string) (Latido, bool) {
	l, ok := LeerLatidoCrudo(root, id)
	if !ok || !l.Vivo() {
		return Latido{}, false
	}
	return l, true
}

// LeerLatidoCrudo devuelve el latido tal como está en disco, vivo o no.
// Lo usa quien necesita distinguir "nunca corrió" de "la corrida murió
// a media tarea".
func LeerLatidoCrudo(root, id string) (Latido, bool) {
	datos, err := os.ReadFile(latidoPath(root, id))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Latido{}, false
		}
		return Latido{}, false
	}
	var l Latido
	if json.Unmarshal(datos, &l) != nil {
		return Latido{}, false
	}
	return l, true
}

// LeerLatidos devuelve los latidos de todas las tareas que corren ahora.
func LeerLatidos(root string, ids []string) map[string]Latido {
	res := make(map[string]Latido, len(ids))
	for _, id := range ids {
		if l, ok := LeerLatido(root, id); ok {
			res[id] = l
		}
	}
	return res
}

// LeerLatidosCrudos devuelve los latidos en disco de varias tareas, vivos
// o no. Lo consume quien tiene que distinguir "nunca corrió" de "la
// corrida murió": con LeerLatidos las dos se ven igual, como un hueco.
func LeerLatidosCrudos(root string, ids []string) map[string]Latido {
	res := make(map[string]Latido, len(ids))
	for _, id := range ids {
		if l, ok := LeerLatidoCrudo(root, id); ok {
			res[id] = l
		}
	}
	return res
}

// Interrumpida dice si la tarea quedó con una corrida muerta encima: hay
// latido en disco pero nadie lo refresca. Es la huella de un SIGKILL, y
// la señal de que `run --reintentar` puede retomarla.
func Interrumpida(root, id string) (Latido, bool) {
	l, ok := LeerLatidoCrudo(root, id)
	if !ok || l.Vivo() {
		return Latido{}, false
	}
	return l, true
}
