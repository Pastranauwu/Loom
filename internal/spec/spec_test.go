package spec

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/task"
)

const specEjemplo = `version: 1
feature: "Autenticación JWT"
agente: backend

reglas:
  - "no usar sesiones en memoria"
  - "tokens con rotación"

tasks:
  - id: T-001
    titulo: modelo de usuario y hash de contraseñas
    porque: seguridad básica
    listo_cuando: go test ./internal/auth/... -run TestHash
    tocar_solo: ["internal/auth/**"]
    no_tocar: ["migrations/**"]
    agente: backend
    peso: liviana
    limite_intentos: 3
    limite_lineas: 200

  - id: T-002
    titulo: endpoint de login
    listo_cuando: go test ./internal/api/... -run TestLogin
    tocar_solo: ["internal/api/**"]
    depende_de: ["T-001"]
    expone: ["POST /api/login -> 200 {token}"]
    peso: media
`

func TestParseSpecCompleto(t *testing.T) {
	s, err := Parse([]byte(specEjemplo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, quiero 1", s.Version)
	}
	if s.Feature != "Autenticación JWT" {
		t.Errorf("Feature = %q, quiero Autenticación JWT", s.Feature)
	}
	if s.Agente != "backend" {
		t.Errorf("Agente = %q, quiero backend", s.Agente)
	}
	if len(s.Reglas) != 2 || s.Reglas[0] != "no usar sesiones en memoria" {
		t.Errorf("Reglas = %v", s.Reglas)
	}
	if len(s.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, quiero 2", len(s.Tasks))
	}
	t1 := s.Tasks[0]
	if t1.ID != "T-001" || t1.Titulo != "modelo de usuario y hash de contraseñas" || t1.Agente != "backend" {
		t.Errorf("T1 = %+v", t1)
	}
	t2 := s.Tasks[1]
	// t2 no definió agente, debe heredar s.Agente ("backend")
	if t2.ID != "T-002" || t2.Agente != "backend" || len(t2.DependeDe) != 1 || t2.DependeDe[0] != "T-001" {
		t.Errorf("T2 = %+v", t2)
	}
}

func TestParseSpecInlineMaps(t *testing.T) {
	raw := `version: 1
feature: "Features inline"
tasks:
  - { id: T-001, titulo: "primera", listo_cuando: "make test", tocar_solo: ["src/**"], agente: "architect" }
  - { id: T-002, titulo: "segunda", listo_cuando: "make test2", depende_de: ["T-001"] }
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse inline: %v", err)
	}
	if len(s.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d, quiero 2", len(s.Tasks))
	}
	if s.Tasks[0].Agente != "architect" {
		t.Errorf("t0 agente = %q, quiero architect", s.Tasks[0].Agente)
	}
}

func TestParseSpecReglasInline(t *testing.T) {
	raw := `version: 1
feature: "Reglas inline"
reglas: ["regla 1", "regla 2"]
tasks:
  - id: T-001
    titulo: algo
    listo_cuando: true
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse reglas inline: %v", err)
	}
	if len(s.Reglas) != 2 || s.Reglas[1] != "regla 2" {
		t.Errorf("Reglas = %v", s.Reglas)
	}
}

func TestAssignCorrelativeIDs(t *testing.T) {
	dir := t.TempDir()
	tasks := []task.Task{
		{Titulo: "tarea a", ListoCuando: "true"},
		{ID: "T-005", Titulo: "tarea existente", ListoCuando: "true"},
		{Titulo: "tarea b", ListoCuando: "true"},
	}

	withIDs, err := AssignCorrelativeIDs(dir, tasks)
	if err != nil {
		t.Fatalf("AssignCorrelativeIDs: %v", err)
	}
	if withIDs[0].ID != "T-001" {
		t.Errorf("t0 ID = %q, quiero T-001", withIDs[0].ID)
	}
	if withIDs[1].ID != "T-005" {
		t.Errorf("t1 ID = %q, quiero T-005", withIDs[1].ID)
	}
	if withIDs[2].ID != "T-002" {
		t.Errorf("t2 ID = %q, quiero T-002", withIDs[2].ID)
	}
}

// La forma rápida: una línea por tarea, mezclable con tareas completas.
// Un titulo con dos puntos sigue siendo titulo.
func TestParseSpecRapido(t *testing.T) {
	s, err := Parse([]byte(`feature: wake on lan
tareas:
  - enviar magic packet por udp
  - "fix: login con tildes"
  - titulo: guardar la mac
    listo_cuando: go test ./internal/store/...
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Tasks) != 3 {
		t.Fatalf("tareas = %d, quiero 3", len(s.Tasks))
	}
	if s.Tasks[0].Titulo != "enviar magic packet por udp" || s.Tasks[0].ListoCuando != "" {
		t.Errorf("t0 = %+v", s.Tasks[0])
	}
	if s.Tasks[1].Titulo != "fix: login con tildes" {
		t.Errorf("t1 titulo = %q", s.Tasks[1].Titulo)
	}
	if s.Tasks[2].ListoCuando != "go test ./internal/store/..." {
		t.Errorf("t2 = %+v", s.Tasks[2])
	}
}

// Con T-001 y T-002 ya en el repo y un spec sin ids, su "T-001" es su
// primera tarea (T-003), no la vieja.
func TestAssignCorrelativeIDsDependenciasPorPosicion(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"T-001", "T-002"} {
		if err := task.Save(dir, task.Task{Version: 1, ID: id, Titulo: "vieja", ListoCuando: "true", LimiteIntentos: 1, LimiteLineas: 1}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := AssignCorrelativeIDs(dir, []task.Task{
		{Titulo: "base"},
		{Titulo: "encima", DependeDe: []string{"T-001"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "T-003" || !reflect.DeepEqual(got[1].DependeDe, []string{"T-003"}) {
		t.Errorf("ids %s,%s · depende_de %v · quiero T-003,T-004 · [T-003]", got[0].ID, got[1].ID, got[1].DependeDe)
	}
}

func TestApplyYDryRun(t *testing.T) {
	dir := t.TempDir()
	tasksDir := filepath.Join(dir, ".loom", "tasks")

	s := Spec{
		Version: 1,
		Feature: "Test Feature",
		Tasks: []task.Task{
			{Titulo: "t1", ListoCuando: "go test ./...", LimiteIntentos: 3, LimiteLineas: 200},
			{Titulo: "t2", ListoCuando: "go test ./...", LimiteIntentos: 3, LimiteLineas: 200},
		},
	}

	// 1. Dry run no debe escribir archivos en disco
	applied, err := Apply(tasksDir, s, true)
	if err != nil {
		t.Fatalf("Apply dry run: %v", err)
	}
	if len(applied) != 2 || applied[0].ID != "T-001" {
		t.Errorf("applied = %+v", applied)
	}
	if _, err := os.Stat(tasksDir); !os.IsNotExist(err) {
		t.Errorf("dry-run creó el directorio %s", tasksDir)
	}

	// 2. Apply real guarda los archivos
	appliedReal, err := Apply(tasksDir, s, false)
	if err != nil {
		t.Fatalf("Apply real: %v", err)
	}
	if len(appliedReal) != 2 {
		t.Fatalf("len(appliedReal) = %d", len(appliedReal))
	}

	loaded, err := task.Load(tasksDir, "T-001")
	if err != nil {
		t.Fatalf("task.Load T-001: %v", err)
	}
	if loaded.Titulo != "t1" {
		t.Errorf("loaded.Titulo = %q, quiero t1", loaded.Titulo)
	}
}

func TestApplyRechazaContratoInvalido(t *testing.T) {
	dir := t.TempDir()
	s := Spec{
		Version: 1,
		Tasks: []task.Task{
			{Titulo: "", ListoCuando: ""}, // Faltan título y listo_cuando
		},
	}
	if _, err := Apply(dir, s, false); err == nil {
		t.Fatal("Apply debió fallar por contrato inválido")
	}
}

func TestFindSpec(t *testing.T) {
	root := t.TempDir()
	if _, err := Find(root); err == nil {
		t.Fatal("Find debió fallar sin archivos")
	}

	specPath := filepath.Join(root, "loom.spec.yml")
	if err := os.WriteFile(specPath, []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Find(root)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != specPath {
		t.Errorf("Find = %q, quiero %q", got, specPath)
	}
}

func TestMarshalRoundtrip(t *testing.T) {
	original, err := Parse([]byte(specEjemplo))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	marshaled := Marshal(original)
	reparsed, err := Parse(marshaled)
	if err != nil {
		t.Fatalf("reparse: %v\nYAML:\n%s", err, marshaled)
	}
	if !reflect.DeepEqual(original, reparsed) {
		t.Errorf("Roundtrip mismatch:\nOriginal: %+v\nReparsed: %+v", original, reparsed)
	}
}

func TestParseSpecAgentesYShip(t *testing.T) {
	raw := `version: 1
feature: "Corrida declarativa"
agentes: 3
ship: true
tasks:
  - id: T-001
    titulo: algo
    listo_cuando: true
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Agentes != 3 {
		t.Errorf("Agentes = %d, quiero 3", s.Agentes)
	}
	if !s.Ship {
		t.Error("Ship = false, quiero true")
	}
}

func TestParseSpecAgentesInvalidos(t *testing.T) {
	raw := "version: 1\nagentes: 0\ntasks:\n  - { titulo: x, listo_cuando: true }\n"
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("agentes: 0 debió rechazarse")
	}
}

func TestParseRequirementsComoCodigoYAMLAnidado(t *testing.T) {
	s, err := Parse([]byte(`feature: recuperar contraseña
requirements:
  functional:
    - solicitar por email
    - cambiar contraseña con token
  security:
    - token de un solo uso
rules:
  - no modificar sesiones
acceptance:
  integration:
    - criterion: token expirado se rechaza
      command: go test ./internal/auth/... -run TestExpired
    - usuario inexistente no revela la cuenta
constraints:
  no_tocar:
    - internal/session/**
ship: true
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Requirements) != 3 || len(s.Acceptance) != 2 {
		t.Fatalf("requirements=%v acceptance=%v", s.Requirements, s.Acceptance)
	}
	if s.Acceptance[0].Command == "" || len(s.Constraints.NoTocar) != 1 || !s.Ship {
		t.Fatalf("spec incompleto: %+v", s)
	}
}

func TestValidatePlanDetectaErroresEstructurales(t *testing.T) {
	ts := []task.Task{
		{ID: "T-001", Titulo: "a", ListoCuando: "true", TocarSolo: []string{"internal/auth/**"}, DependeDe: []string{"T-002"}, Usa: []string{"auth.Missing()"}},
		{ID: "T-002", Titulo: "b", ListoCuando: "true", TocarSolo: []string{"internal/auth/token.go"}, DependeDe: []string{"T-001"}},
		{ID: "T-003", Titulo: "c", ListoCuando: "true", TocarSolo: []string{"internal/auth/token.go"}},
	}
	issues := ValidatePlan(Spec{}, ts)
	var codes []string
	for _, i := range issues {
		if i.Level == "error" {
			codes = append(codes, i.Code)
		}
	}
	joined := strings.Join(codes, ",")
	for _, want := range []string{"orphan_interface", "cycle", "write_overlap"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta %s en %v", want, issues)
		}
	}
}

// la base de la clase y "def" no cambian el
// contrato; el tipo de retorno sí
func TestValidatePlanComparaFirmasSinBasesNiDef(t *testing.T) {
	ts := []task.Task{
		{ID: "T-001", Titulo: "a", ListoCuando: "true", TocarSolo: []string{"a.py"}, Expone: []string{"class Garment(Base): id, filename", "get_db() -> Iterator[Session]"}},
		{ID: "T-002", Titulo: "b", ListoCuando: "true", TocarSolo: []string{"b.py"}, DependeDe: []string{"T-001"}, Usa: []string{"class Garment: id, filename", "def get_db() -> Iterator[Session]"}},
		{ID: "T-003", Titulo: "c", ListoCuando: "true", TocarSolo: []string{"c.py"}, DependeDe: []string{"T-001"}, Usa: []string{"get_db() -> Generator[Session]"}},
	}
	var errs []string
	for _, i := range ValidatePlan(Spec{}, ts) {
		if i.Level == "error" {
			errs = append(errs, i.Message)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0], "T-003") {
		t.Errorf("errores = %v, quiero solo el Generator de T-003", errs)
	}
}

func TestMarshalRoundtripAgentesYShip(t *testing.T) {
	raw := `version: 1
feature: "Roundtrip"
agentes: 4
ship: true
tasks:
  - id: T-001
    titulo: algo
    listo_cuando: true
`
	original, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	reparsed, err := Parse(Marshal(original))
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	if reparsed.Agentes != 4 || !reparsed.Ship {
		t.Errorf("roundtrip perdió agentes/ship: %+v", reparsed)
	}
}

// Las reglas de la especificación llegan al agente por las notas de la
// tarea: al aplicar, se anteponen al enfoque propio sin pisarlo.
func TestApplyInyectaReglasEnNotas(t *testing.T) {
	dir := t.TempDir()
	s := Spec{
		Version: 1,
		Reglas:  []string{"tokens stateless", "sin sesiones en memoria"},
		Tasks: []task.Task{
			{Titulo: "con notas", ListoCuando: "true", Notas: "empieza por bcrypt"},
			{Titulo: "sin notas", ListoCuando: "true"},
		},
	}
	applied, err := Apply(filepath.Join(dir, "tasks"), s, true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	n0 := applied[0].Notas
	if !strings.Contains(n0, "Reglas de la especificación:") ||
		!strings.Contains(n0, "- tokens stateless") ||
		!strings.Contains(n0, "- sin sesiones en memoria") ||
		!strings.Contains(n0, "empieza por bcrypt") {
		t.Errorf("notas con reglas y enfoque:\n%s", n0)
	}
	if !strings.Contains(applied[1].Notas, "- tokens stateless") {
		t.Errorf("tarea sin notas también recibe las reglas:\n%s", applied[1].Notas)
	}
	// el spec no se contamina: Marshal sigue escribiendo reglas arriba,
	// no dentro de cada tarea
	if len(s.Tasks[0].Notas) != 0 && strings.Contains(s.Tasks[0].Notas, "Reglas") {
		t.Error("Apply mutó las notas del spec original")
	}
}

func TestParseSpecLimites(t *testing.T) {
	raw := `version: 1
feature: "Feature con limites globales"
limites:
  intentos: 5
  lineas: 500
tasks:
  - id: T-001
    titulo: "tarea con limites globales"
    listo_cuando: "true"
  - id: T-002
    titulo: "tarea con limite especifico"
    listo_cuando: "true"
    limite_intentos: 2
    limite_lineas: 100
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse limites: %v", err)
	}
	if s.Limites.Intentos != 5 || s.Limites.Lineas != 500 {
		t.Errorf("Limites = %+v", s.Limites)
	}
	if len(s.Tasks) != 2 {
		t.Fatalf("len(Tasks) = %d", len(s.Tasks))
	}
	// T1 hereda limites globales
	if s.Tasks[0].LimiteIntentos != 5 || s.Tasks[0].LimiteLineas != 500 {
		t.Errorf("T1 limites = %d, %d; quiero 5, 500", s.Tasks[0].LimiteIntentos, s.Tasks[0].LimiteLineas)
	}
	// T2 mantiene sus límites específicos
	if s.Tasks[1].LimiteIntentos != 2 || s.Tasks[1].LimiteLineas != 100 {
		t.Errorf("T2 limites = %d, %d; quiero 2, 100", s.Tasks[1].LimiteIntentos, s.Tasks[1].LimiteLineas)
	}
}

// El humano declara constraints y ninguna task: el planificador produce el IR
// completo DESPUÉS de parsear, así que la restricción no puede aplicarse al
// leer el YAML sin perderse. Aquí se imita ese camino: Parse deja cero tareas y
// los contratos se agregan como los agrega planearRequirements.
func TestApplyPropagaConstraintsAlIRGenerado(t *testing.T) {
	raw := `version: 1
feature: recuperación de contraseña
requirements:
  - solicitar recuperación por email
  - cambiar la contraseña usando el token
constraints:
  no_tocar:
    - internal/session/**
    - migrations/**
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Tasks) != 0 {
		t.Fatalf("un spec de requirements no trae tasks: %d", len(s.Tasks))
	}
	if len(s.Constraints.NoTocar) != 2 {
		t.Fatalf("Constraints = %+v", s.Constraints)
	}

	s.Tasks = append(s.Tasks,
		task.Task{Version: task.Version, Titulo: "solicitud de recuperación", ListoCuando: "true", TocarSolo: []string{"internal/recuperacion/**"}},
		task.Task{Version: task.Version, Titulo: "cambio con token", ListoCuando: "true", TocarSolo: []string{"internal/token/**"}, NoTocar: []string{"docs/**"}},
	)

	applied, err := Apply(filepath.Join(t.TempDir(), "tasks"), s, true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("len(applied) = %d", len(applied))
	}
	for _, got := range applied {
		for _, glob := range s.Constraints.NoTocar {
			if !contiene(got.NoTocar, glob) {
				t.Errorf("%s (%s) no recibió %q · no_tocar = %v", got.ID, got.Titulo, glob, got.NoTocar)
			}
		}
	}
	// lo que la tarea ya traía no se pierde ni se duplica
	if !contiene(applied[1].NoTocar, "docs/**") || len(applied[1].NoTocar) != 3 {
		t.Errorf("no_tocar propio + globales = %v", applied[1].NoTocar)
	}
	// y el spec original no queda contaminado
	if len(s.Tasks[0].NoTocar) != 0 {
		t.Errorf("Apply mutó el no_tocar del spec original: %v", s.Tasks[0].NoTocar)
	}
}

// Las tasks escritas a mano en el YAML siguen recibiéndolas, aunque la
// aplicación ya no ocurra en Parse.
func TestApplyPropagaConstraintsATasksDelYAML(t *testing.T) {
	raw := `version: 1
feature: exportación
constraints:
  no_tocar: ["internal/session/**"]
tasks:
  - id: T-001
    titulo: generador csv
    listo_cuando: "true"
    tocar_solo: ["internal/export/**"]
`
	s, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	applied, err := Apply(filepath.Join(t.TempDir(), "tasks"), s, true)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !contiene(applied[0].NoTocar, "internal/session/**") {
		t.Errorf("no_tocar = %v", applied[0].NoTocar)
	}
}

func contiene(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// El spec exportado tiene que traer la frontera global: sin constraints, un
// plan --export-spec entrega un spec que miente sobre lo que nadie puede tocar.
func TestMarshalEscribeConstraints(t *testing.T) {
	s := Spec{
		Version:     1,
		Feature:     "recuperación de contraseña",
		Constraints: Constraints{NoTocar: []string{"internal/session/**", "migrations/**"}},
		Tasks: []task.Task{
			{Version: task.Version, ID: "T-001", Titulo: "solicitud", ListoCuando: "true", TocarSolo: []string{"internal/recuperacion/**"}},
		},
	}
	out := Marshal(s)
	vuelta, err := Parse(out)
	if err != nil {
		t.Fatalf("Parse del spec exportado: %v\n%s", err, out)
	}
	if !reflect.DeepEqual(vuelta.Constraints.NoTocar, s.Constraints.NoTocar) {
		t.Errorf("constraints tras la vuelta = %v, quiero %v\n%s", vuelta.Constraints.NoTocar, s.Constraints.NoTocar, out)
	}
}

func TestFindSpecPlural(t *testing.T) {
	for _, nombre := range []string{"loom.specs.yml", "loom.specs.yaml"} {
		t.Run(nombre, func(t *testing.T) {
			root := t.TempDir()
			p := filepath.Join(root, nombre)
			if err := os.WriteFile(p, []byte("feature: snake\n"), 0644); err != nil {
				t.Fatal(err)
			}
			got, err := Find(root)
			if err != nil || got != p {
				t.Fatalf("Find = %q, %v", got, err)
			}
		})
	}
}

// Un plan nuevo sobre un proyecto en marcha consume lo que expusieron las
// tareas que ya están en el repo: measureText de T-003 no es un usa huérfano.
func TestApplyCuentaLasTareasPrevias(t *testing.T) {
	dir := t.TempDir()
	previa := task.Task{Version: task.Version, ID: "T-001", Titulo: "pixel", ListoCuando: "node --test src/pixel.test.js", TocarSolo: []string{"src/pixel.js"}, Expone: []string{"measureText(text: string, scale: number) => number"}}
	if err := task.Save(dir, previa); err != nil {
		t.Fatal(err)
	}
	s := Spec{Version: 1, Tasks: []task.Task{{ID: "T-002", Titulo: "hud", ListoCuando: "node --test src/hud.test.js", TocarSolo: []string{"src/hud.js"}, DependeDe: []string{"T-001"}, Usa: []string{"measureText(text: string, scale: number) => number"}}}}
	got, err := Apply(dir, s, true)
	if err != nil {
		t.Fatalf("la tarea previa expone lo que la nueva usa: %v", err)
	}
	if got[0].ID != "T-002" {
		t.Errorf("id = %s", got[0].ID)
	}
}

// el esqueleto crea los stubs que después rellena cada tarea: compartir
// archivos con quien depende de ti no es solapamiento.
func TestValidatePlanPermiteCompartirConDependencia(t *testing.T) {
	ts := []task.Task{
		{ID: "T-001", Titulo: "esqueleto", ListoCuando: "true", TocarSolo: []string{"src/**"}},
		{ID: "T-002", Titulo: "a", ListoCuando: "true", TocarSolo: []string{"src/a.ts"}, DependeDe: []string{"T-001"}},
		{ID: "T-003", Titulo: "b", ListoCuando: "true", TocarSolo: []string{"src/b.ts"}, DependeDe: []string{"T-001"}},
	}
	for _, i := range ValidatePlan(Spec{}, ts) {
		if i.Code == "write_overlap" {
			t.Errorf("solapamiento falso: %s", i.Message)
		}
	}
}

func TestIDsDeRequerimientos(t *testing.T) {
	s, err := Parse([]byte("feature: x\nrequirements:\n  - Fondo con degradado\n  - id: nav\n    texto: Barra flotante\n  functional_extra: nada\n"))
	if err == nil {
		t.Fatalf("una lista con un mapeo suelto no es válida: %+v", s)
	}
	s, err = Parse([]byte("feature: x\nrequirements:\n  - Fondo con degradado\n  - id: nav\n    texto: Barra flotante\n"))
	if err != nil {
		t.Fatal(err)
	}
	ids := s.IDs()
	if len(s.Requirements) != 2 || s.Requirements[1] != "Barra flotante" || ids[1] != "nav" {
		t.Fatalf("%v %v", s.Requirements, ids)
	}
	if ids[0] != IDRequerimiento("fondo  con degradado ") || ids[0][:2] != "R-" {
		t.Fatalf("el id del texto no es estable: %q", ids[0])
	}
}
