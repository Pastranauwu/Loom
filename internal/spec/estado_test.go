package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func specBase() Spec {
	return Spec{
		Feature:        "gastos",
		Requirements:   []string{"registrar un gasto", "fondo gris"},
		RequirementIDs: []string{"", "fondo"},
		Reglas:         []string{"sin dependencias externas"},
		Acceptance:     []Acceptance{{Criterion: "suite verde", Command: "go test ./..."}},
		Constraints:    Constraints{NoTocar: []string{"vendor/**"}},
	}
}

func TestDiffPorClase(t *testing.T) {
	prev := Snapshot(specBase())
	s := specBase()
	s.Requirements = []string{"registrar un gasto", "fondo azul", "listar gastos"}
	s.RequirementIDs = []string{"", "fondo", ""}
	s.Reglas = []string{"stdlib solamente"}
	s.Acceptance = []Acceptance{{Criterion: "suite verde", Command: "go test -race ./..."}}
	s.Constraints = Constraints{}
	d := Diff(prev, Snapshot(s))

	if got := d.De(ClaseRequirement); len(got.Agregado) != 1 || len(got.Modificado) != 1 || got.Modificado[0].Clave != "fondo" || len(got.Eliminado) != 0 {
		t.Errorf("requirements = %+v", got)
	}
	if got := d.De(ClaseRegla); len(got.Agregado) != 1 || len(got.Eliminado) != 1 {
		t.Errorf("reescribir una regla sin id es eliminar + agregar: %+v", got)
	}
	if got := d.De(ClaseAceptacion); len(got.Modificado) != 1 || len(got.Agregado)+len(got.Eliminado) != 0 {
		t.Errorf("cambiar el comando es modificar: %+v", got)
	}
	if got := d.De(ClaseRestriccion); len(got.Eliminado) != 1 {
		t.Errorf("restricciones = %+v", got)
	}
	if !strings.Contains(d.String(), "~ requirement fondo") || !strings.Contains(d.String(), "- constraint vendor/**") {
		t.Errorf("salida:\n%s", d)
	}
}

func TestDiffIgualEsVacio(t *testing.T) {
	if !Diff(Snapshot(specBase()), Snapshot(specBase())).Vacio() {
		t.Error("el mismo spec no debe dar delta")
	}
}

func TestPrevioMigraDeIntencionYFeature(t *testing.T) {
	root := t.TempDir()
	s := specBase()
	if _, ok := Previo(root, "loom.spec.yml", s); ok {
		t.Fatal("sin nada guardado no hay previo")
	}
	os.MkdirAll(filepath.Join(root, ".loom"), 0o755)
	if err := SaveIntencion(root, IntencionDe(s)); err != nil {
		t.Fatal(err)
	}
	prev, ok := Previo(root, "loom.spec.yml", s)
	if !ok || !Diff(prev, Snapshot(s)).Vacio() {
		t.Errorf("intencion.json debe sembrar el estado: ok=%v", ok)
	}
	if err := SaveEstadoSpec(root, "loom.spec.yml", Snapshot(Spec{Feature: "x", Requirements: []string{"solo esto"}})); err != nil {
		t.Fatal(err)
	}
	if prev, _ := Previo(root, "loom.spec.yml", s); len(prev.Piezas[ClaseRequirement]) != 1 {
		t.Error("state.json manda sobre intencion.json")
	}
}

func TestQuitarEstadoConservaLosDemasSpecs(t *testing.T) {
	root := t.TempDir()
	_ = SaveEstadoSpec(root, "a.yml", Snapshot(specBase()))
	_ = SaveEstadoSpec(root, "specs/b.yml", Snapshot(specBase()))
	_ = QuitarEstado(root, "a.yml")
	if _, ok := LoadEstado(root).Specs["specs/b.yml"]; !ok {
		t.Error("borró el estado de otro spec")
	}
	_ = QuitarEstado(root, "specs/b.yml")
	if _, err := os.Stat(filepath.Join(root, ".loom", EstadoFile)); !os.IsNotExist(err) {
		t.Error("sin specs el archivo debe desaparecer")
	}
}

func TestFindAll(t *testing.T) {
	root := t.TempDir()
	if len(FindAll(root)) != 0 {
		t.Fatal("repo sin specs")
	}
	os.MkdirAll(filepath.Join(root, "specs"), 0o755)
	for _, f := range []string{"specs/b.yml", "specs/a.yaml", "loom.spec.yml"} {
		os.WriteFile(filepath.Join(root, f), []byte("feature: x\n"), 0o644)
	}
	got := FindAll(root)
	var rel []string
	for _, p := range got {
		r, _ := filepath.Rel(root, p)
		rel = append(rel, r)
	}
	if strings.Join(rel, " ") != "loom.spec.yml specs/b.yml specs/a.yaml" {
		t.Errorf("orden = %v", rel)
	}
}

func TestFusionarAceptacion(t *testing.T) {
	root := t.TempDir()
	viejo := Spec{Acceptance: []Acceptance{{Criterion: "de a", Command: "a"}, {Criterion: "de b", Command: "b"}}}
	prevB := Snapshot(Spec{Acceptance: []Acceptance{{Criterion: "de b", Command: "b"}}})
	ahoraB := Spec{Acceptance: []Acceptance{{Criterion: "de b", Command: "b2"}}}
	_ = SaveFeatureState(root, ahoraB)
	if err := FusionarAceptacion(root, viejo, prevB, ahoraB); err != nil {
		t.Fatal(err)
	}
	f, _ := LoadFeatureState(root)
	if len(f.Acceptance) != 2 || f.Acceptance[0].Criterion != "de a" || f.Acceptance[1].Command != "b2" {
		t.Errorf("aceptación = %+v", f.Acceptance)
	}
}
