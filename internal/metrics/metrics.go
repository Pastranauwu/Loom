package metrics

import (
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// Metricas es el reporte de las cinco métricas. Los punteros nulos (null) marcan lo que
// no tiene datos todavía, en vez de un número inventado.
type Metricas struct {
	IntentosHastaVerde float64  `json:"intentos_hasta_verde"`
	Ruido              float64  `json:"ruido_pct"`
	Roce               float64  `json:"roce_por_10"`
	Friccion           *float64 `json:"friccion_min"`
	RechazoEntrada     float64  `json:"rechazo_entrada_pct"`
	Tokens             int      `json:"tokens"`
	Entregadas         int      `json:"entregadas"`
}

// Datos es lo que Calcular necesita, ya leído del disco.
type Datos struct {
	Tasks    []task.Task
	Estados  map[string]state.State
	Attempts map[string][]loop.Attempt
	Entregas []Entrega
}

// Calcular aplica las cinco métricas sobre los artefactos del repo:
// attempts.jsonl y las entregas. Friccion NO se llena acá y queda en
// null: su fuente es el ciclo de revisión del PR, que vive en GitHub y no
// en el disco. La pone Friccion() aparte, para que esto siga siendo una
// función pura que no toca la red.
func Calcular(d Datos) Metricas {
	m := Metricas{}

	// intentos hasta verde: promedio de intentos de las tareas entregadas
	var suma, cuenta int
	for id, est := range d.Estados {
		if est.Estado != state.Lista {
			continue
		}
		if n := len(d.Attempts[id]); n > 0 {
			suma += n
			cuenta++
		}
	}
	if cuenta > 0 {
		m.IntentosHastaVerde = redondear(float64(suma)/float64(cuenta), 1)
		m.Entregadas = cuenta
	}

	// ruido: hallazgos de ruido sobre líneas añadidas
	var ruido, lineas int
	for _, e := range d.Entregas {
		ruido += e.Ruido
		lineas += e.LineasMas
	}
	if lineas > 0 {
		m.Ruido = redondear(float64(ruido)/float64(lineas)*100, 1)
	}

	// roce: conflictos de rebase por cada 10 entregas
	if n := len(d.Entregas); n > 0 {
		conflictos := 0
		for _, e := range d.Entregas {
			if e.Conflicto {
				conflictos++
			}
		}
		m.Roce = redondear(float64(conflictos)/float64(n)*10, 1)
	}

	// rechazo en entrada: contratos que no validan sobre el total
	total := len(d.Tasks)
	invalidos := 0
	for _, t := range d.Tasks {
		if len(t.Validate()) > 0 {
			invalidos++
		}
	}
	if total > 0 {
		m.RechazoEntrada = redondear(float64(invalidos)/float64(total)*100, 1)
	}

	// costo en tokens: el gasto (con caché ponderada) de todos los intentos
	for _, attempts := range d.Attempts {
		for _, a := range attempts {
			m.Tokens += a.Tokens.Gasto()
		}
	}

	return m
}

func redondear(v float64, decimales int) float64 {
	pot := 1.0
	for i := 0; i < decimales; i++ {
		pot *= 10
	}
	return float64(int(v*pot+0.5)) / pot
}
