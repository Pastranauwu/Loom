package revisor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

type genFijo struct {
	texto string
	err   error
}

func (g genFijo) Generar(context.Context, string) (string, error) { return g.texto, g.err }

func tareas() []task.Task {
	return []task.Task{
		{ID: "T-001", Titulo: "validar mac", ListoCuando: "go test ./...", Expone: []string{"wol.Parse(s string) error"}},
		{ID: "T-002", Titulo: "empaquetar", ListoCuando: "docker build ."},
	}
}

const informeOK = `{"tareas":[
 {"id":"T-001","tipo":"feat","funciona":true,"descripcion":"parsea y valida una MAC"},
 {"id":"T-002","tipo":"chore","funciona":true,"descripcion":"Dockerfile y compose"}
]}`

func TestRevisarInformaPorTarea(t *testing.T) {
	v, err := Revisar(context.Background(), Opciones{
		Generador: genFijo{texto: "```json\n" + informeOK + "\n```"},
		Tareas:    tareas(), Diff: "+ algo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !v.Aprobado() || len(v.Tareas) != 2 {
		t.Fatalf("v = %+v", v)
	}
	// el tipo sale del revisor, no de adivinar por el título: empaquetar
	// es chore, no feat
	if v.Tareas[1].Tipo != "chore" {
		t.Errorf("tipo = %q, quiero chore", v.Tareas[1].Tipo)
	}
}

func TestInformeListaTareasYCambios(t *testing.T) {
	v, err := Revisar(context.Background(), Opciones{
		Generador: genFijo{texto: `{"tareas":[
 {"id":"T-001","tipo":"feat","funciona":false,"descripcion":"parsea una MAC","cambios":["wol.go:12 — acepta mac vacía"]},
 {"id":"T-002","tipo":"chore","funciona":true,"descripcion":"Dockerfile"}
],"notas":"las dos usan formatos de MAC distintos"}`},
		Tareas: tareas(), Diff: "+ algo",
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Aprobado() {
		t.Error("con una tarea rota no puede estar aprobado")
	}
	inf := v.Informe()
	for _, quiero := range []string{
		"T-001", "`feat`", "**no**", "parsea una MAC", // la tabla
		"wol.go:12", "qué cambiar", // qué corregir, con archivo:línea
		"formatos de MAC distintos", // lo que solo se ve en el conjunto
		"El revisor pide cambios",   // el pie, cuando hay algo que corregir
	} {
		if !strings.Contains(inf, quiero) {
			t.Errorf("el informe no menciona %q", quiero)
		}
	}
	// y con todo en orden, el pie recuerda de quién es la decisión
	ok, _ := Revisar(context.Background(), Opciones{
		Generador: genFijo{texto: informeOK}, Tareas: tareas(), Diff: "+x",
	})
	if !strings.Contains(ok.Informe(), "La aprobación es tuya") {
		t.Error("el informe debe dejar claro que aprobar es del humano")
	}
	if !strings.Contains(v.Resumen(), "T-001") {
		t.Errorf("resumen = %q", v.Resumen())
	}
}

// Falla cerrado: lo que no se puede leer no se da por revisado, porque
// quien aprueba lo haría a ciegas.
func TestRevisarFallaCerrado(t *testing.T) {
	casos := []struct {
		nombre string
		o      Opciones
	}{
		{"sin generador", Opciones{Tareas: tareas(), Diff: "+x"}},
		{"sin diff", Opciones{Generador: genFijo{texto: informeOK}, Tareas: tareas()}},
		{"el modelo falla", Opciones{Generador: genFijo{err: errors.New("503")}, Tareas: tareas(), Diff: "+x"}},
		{"respuesta ilegible", Opciones{Generador: genFijo{texto: "pues no sé"}, Tareas: tareas(), Diff: "+x"}},
		{"json inválido", Opciones{Generador: genFijo{texto: "{tareas: si}"}, Tareas: tareas(), Diff: "+x"}},
		{"sin tareas", Opciones{Generador: genFijo{texto: `{"tareas":[]}`}, Tareas: tareas(), Diff: "+x"}},
		{"diff enorme", Opciones{Generador: genFijo{texto: informeOK}, Tareas: tareas(), Diff: strings.Repeat("x", MaxDiff+1)}},
		{"tipo inventado", Opciones{Generador: genFijo{
			texto: `{"tareas":[{"id":"T-001","tipo":"mejora","funciona":true,"descripcion":"x"},{"id":"T-002","tipo":"chore","funciona":true,"descripcion":"y"}]}`},
			Tareas: tareas(), Diff: "+x"}},
		{"sin descripción", Opciones{Generador: genFijo{
			texto: `{"tareas":[{"id":"T-001","tipo":"feat","funciona":true,"descripcion":""},{"id":"T-002","tipo":"chore","funciona":true,"descripcion":"y"}]}`},
			Tareas: tareas(), Diff: "+x"}},
		{"rota sin decir qué cambiar", Opciones{Generador: genFijo{
			texto: `{"tareas":[{"id":"T-001","tipo":"feat","funciona":false,"descripcion":"x"},{"id":"T-002","tipo":"chore","funciona":true,"descripcion":"y"}]}`},
			Tareas: tareas(), Diff: "+x"}},
		{"se salta una tarea", Opciones{Generador: genFijo{
			texto: `{"tareas":[{"id":"T-001","tipo":"feat","funciona":true,"descripcion":"x"}]}`},
			Tareas: tareas(), Diff: "+x"}},
	}
	for _, c := range casos {
		if v, err := Revisar(context.Background(), c.o); err == nil || v.Aprobado() {
			t.Errorf("%s: v = %+v, err = %v · debe fallar sin darse por revisado", c.nombre, v, err)
		}
	}
}

// El prompt tiene que decirle qué juzga y qué no: si marca como rota
// cualquier cosa por estilo, nunca se aprueba nada.
func TestPromptAcotaElJuicio(t *testing.T) {
	p := Prompt(tareas(), "+ codigo")
	for _, quiero := range []string{"T-001", "validar mac", "wol.Parse", "+ codigo",
		"Do NOT mark works:false for style", "chore", "description", "changes"} {
		if !strings.Contains(p, quiero) {
			t.Errorf("el prompt no menciona %q", quiero)
		}
	}
}

func TestPromptLlevaObsoleto(t *testing.T) {
	tk := task.Task{ID: "T-018", Titulo: "x", ListoCuando: "go test ./...",
		Notas: "contexto\n\nObsolete: la aserción de test_panel que espera 3 columnas\nahora son 4\n\notra cosa"}
	p := Prompt([]task.Task{tk}, "")
	if !strings.Contains(p, "declared Obsolete (these test changes are allowed): la aserción de test_panel que espera 3 columnas ahora son 4") {
		t.Fatalf("el prompt no lleva lo obsoleto:\n%s", p)
	}
	if strings.Contains(p, "otra cosa") {
		t.Fatal("se llevó texto fuera de la sección")
	}
}
