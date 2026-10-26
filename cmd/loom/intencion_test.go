package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestDeltaRequirements(t *testing.T) {
	d := deltaRequirements(
		reqsDe([]string{"login", "exportar csv", "reporte pdf", "fondo gris"}, []string{"", "", "", "fondo"}),
		reqsDe([]string{"login", "exportar csv con filtros", "reporte pdf", "fondo azul violeta"}, []string{"", "", "", "fondo"}),
	)
	textos := func(rs []req) string {
		var out []string
		for _, r := range rs {
			out = append(out, r.Texto)
		}
		return strings.Join(out, "|")
	}
	if textos(d.nuevos) != "exportar csv con filtros" || textos(d.hechos) != "login|reporte pdf" || textos(d.retirados) != "exportar csv" {
		t.Fatalf("%+v", d)
	}
	// con id explícito, reescribir no es quitar + agregar: es un cambio
	if len(d.cambiados) != 1 || d.cambiados[0].antes.Texto != "fondo gris" || d.cambiados[0].ahora.Texto != "fondo azul violeta" {
		t.Fatalf("cambiados = %+v", d.cambiados)
	}
	if d := deltaRequirements(nil, reqsDe([]string{"a"}, nil)); len(d.nuevos) != 1 {
		t.Fatalf("sin plan previo todo es nuevo: %+v", d)
	}
}

// al cambiar un requerimiento, el arquitecto recibe qué lo implementó
func TestCubiertoPor(t *testing.T) {
	tareas := []task.Task{{ID: "T-012", Titulo: "fondo", TocarSolo: []string{"src/styles.css"}, Cubre: []string{"fondo"}}, {ID: "T-013", Cubre: []string{"otro"}}}
	if got := cubiertoPor(tareas, "fondo"); !strings.Contains(got, "T-012") || !strings.Contains(got, "src/styles.css") || strings.Contains(got, "T-013") {
		t.Fatalf("%q", got)
	}
}

// un up sobre un spec de requirements que no cambió no llama al
// arquitecto: antes lo pagaba otra vez y duplicaba las tareas
func TestSpecSinCambiosNoReplanea(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(root, "loom.spec.yml")
	yml := "feature: gastos\nrequirements:\n  - registrar un gasto\n  - listar gastos del mes\n"
	if err := os.WriteFile(ruta, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := spec.Load(ruta)
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.SaveIntencion(root, spec.IntencionDe(s)); err != nil {
		t.Fatal(err)
	}
	// sin config ni ejecutor: si intentara planear, fallaría
	got, err := runApply(root, ruta, false, false)
	if err != nil {
		t.Fatalf("replaneó un spec sin cambios: %v", err)
	}
	if got.Feature != "gastos" {
		t.Errorf("feature = %q", got.Feature)
	}
	if es, _ := os.ReadDir(filepath.Join(root, ".loom", "tasks")); len(es) != 0 {
		t.Errorf("escribió %d tareas", len(es))
	}
}

// un feature planeado antes de intencion.json: su feature.json trae los
// mismos requirements y además la aceptación del esqueleto
func TestSpecSinIntencionUsaFeatureJSON(t *testing.T) {
	out = ui.New(io.Discard, false)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	ruta := filepath.Join(root, "loom.spec.yml")
	yml := "feature: gastos\nrequirements:\n  - registrar un gasto\nacceptance:\n  - suite verde\n"
	if err := os.WriteFile(ruta, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := spec.Load(ruta)
	if err != nil {
		t.Fatal(err)
	}
	s.Acceptance = append(s.Acceptance, spec.Acceptance{Criterion: "flujo de punta a punta del esqueleto", Command: "bash e2e.sh"})
	if err := spec.SaveFeatureState(root, s); err != nil {
		t.Fatal(err)
	}
	if _, err := runApply(root, ruta, false, false); err != nil {
		t.Fatalf("replaneó un spec ya planeado: %v", err)
	}
}
