package metrics

import (
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

func tareaValida(id string) task.Task {
	return task.Task{
		Version:        task.Version,
		ID:             id,
		Titulo:         "x",
		ListoCuando:    "true",
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}
}

func TestCalcularVacio(t *testing.T) {
	m := Calcular(Datos{})
	if m.IntentosHastaVerde != 0 || m.Ruido != 0 || m.Roce != 0 || m.RechazoEntrada != 0 || m.Tokens != 0 {
		t.Fatalf("datos vacíos = %+v, quiero ceros", m)
	}
	if m.Friccion != nil {
		t.Error("fricción debió ser null sin datos")
	}
}

func TestCalcularIntentosHastaVerde(t *testing.T) {
	d := Datos{
		Estados: map[string]state.State{
			"T-001": {ID: "T-001", Estado: state.Lista, Intentos: 1},
			"T-002": {ID: "T-002", Estado: state.Lista, Intentos: 3},
			"T-003": {ID: "T-003", Estado: state.Detenida, Intentos: 5},
		},
		Attempts: map[string][]loop.Attempt{
			"T-001": {{Intento: 1}},
			"T-002": {{Intento: 1}, {Intento: 2}, {Intento: 3}},
			"T-003": {{Intento: 1}, {Intento: 2}, {Intento: 3}, {Intento: 4}, {Intento: 5}},
		},
	}
	m := Calcular(d)
	if m.IntentosHastaVerde != 2.0 {
		t.Errorf("intentos hasta verde = %v, quiero 2.0", m.IntentosHastaVerde)
	}
	if m.Entregadas != 2 {
		t.Errorf("entregadas = %d, quiero 2", m.Entregadas)
	}
}

func TestCalcularRuidoYRoce(t *testing.T) {
	d := Datos{
		Entregas: []Entrega{
			{ID: "T-001", LineasMas: 100, Ruido: 4},
			{ID: "T-002", LineasMas: 100, Ruido: 6, Conflicto: true},
			{ID: "T-003", LineasMas: 200, Ruido: 0},
			{ID: "T-004", LineasMas: 0, Ruido: 0},
		},
	}
	m := Calcular(d)
	if m.Ruido != 2.5 {
		t.Errorf("ruido = %v, quiero 2.5 (10 de 400)", m.Ruido)
	}
	if m.Roce != 2.5 {
		t.Errorf("roce = %v, quiero 2.5 (1 de 4)", m.Roce)
	}
}

func TestCalcularRechazoEntrada(t *testing.T) {
	d := Datos{
		Tasks: []task.Task{
			tareaValida("T-001"),
			tareaValida("T-002"),
			{ID: "T-003"}, // inválido: sin version ni listo_cuando
			{ID: "T-004"},
		},
	}
	m := Calcular(d)
	if m.RechazoEntrada != 50.0 {
		t.Errorf("rechazo = %v, quiero 50.0", m.RechazoEntrada)
	}
}

func TestCalcularTokens(t *testing.T) {
	d := Datos{
		Attempts: map[string][]loop.Attempt{
			"T-001": {
				{Tokens: loop.Tokens{Entrada: 100, Salida: 50}},
				{Tokens: loop.Tokens{Entrada: 200, Salida: 100}},
			},
		},
	}
	if m := Calcular(d); m.Tokens != 450 {
		t.Errorf("tokens = %d, quiero 450", m.Tokens)
	}
}

func TestGuardarYListarEntregas(t *testing.T) {
	root := t.TempDir()
	e := Entrega{ID: "T-001", Fecha: time.Now(), LineasMas: 10, Ruido: 1, Aprobado: true, PR: "https://x/1"}
	if err := GuardarEntrega(root, e); err != nil {
		t.Fatal(err)
	}
	if err := GuardarEntrega(root, Entrega{ID: "T-002", Conflicto: true}); err != nil {
		t.Fatal(err)
	}
	lista, err := ListarEntregas(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 2 {
		t.Fatalf("entregas = %d, quiero 2", len(lista))
	}
	if lista[0].ID != "T-001" || lista[0].LineasMas != 10 || lista[0].PR != "https://x/1" {
		t.Errorf("primera entrega = %+v", lista[0])
	}
	if !lista[1].Conflicto {
		t.Errorf("segunda entrega debió tener conflicto: %+v", lista[1])
	}
}
