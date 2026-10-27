package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/gate"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func tarea(id string, alcance []string) task.Task {
	return task.Task{ID: id, Titulo: id, TocarSolo: alcance}
}

func TestAsignarUnaSola(t *testing.T) {
	aceptadas, rechazos := asignar([]task.Task{tarea("T-001", nil)})
	if len(aceptadas) != 1 || len(rechazos) != 0 {
		t.Fatalf("aceptadas=%v rechazos=%v, quiero una sola aceptada", aceptadas, rechazos)
	}
}

func TestAsignarVacioConMasDeUna(t *testing.T) {
	aceptadas, rechazos := asignar([]task.Task{
		tarea("T-001", []string{"src/export/**"}),
		tarea("T-002", nil),
	})
	if len(aceptadas) != 1 || aceptadas[0].ID != "T-001" {
		t.Fatalf("aceptadas=%v, quiero solo T-001", aceptadas)
	}
	if len(rechazos) != 1 || rechazos[0].ID != "T-002" {
		t.Fatalf("rechazos=%v, quiero T-002 por vacío", rechazos)
	}
}

func TestAsignarCruce(t *testing.T) {
	aceptadas, rechazos := asignar([]task.Task{
		tarea("T-001", []string{"src/**"}),
		tarea("T-002", []string{"src/export/**"}),
		tarea("T-003", []string{"docs/**"}),
	})
	ids := map[string]bool{}
	for _, a := range aceptadas {
		ids[a.ID] = true
	}
	if len(aceptadas) != 2 || !ids["T-001"] || !ids["T-003"] {
		t.Fatalf("aceptadas=%v, quiero T-001 y T-003", aceptadas)
	}
	if len(rechazos) != 1 || rechazos[0].ID != "T-002" {
		t.Fatalf("rechazos=%v, quiero T-002 por cruce", rechazos)
	}
}

func TestDepsVerdes(t *testing.T) {
	verde := map[string]bool{"T-001": true}
	if !depsVerdes(nil, verde) {
		t.Error("sin dependencias debió ser verde")
	}
	if !depsVerdes([]string{"T-001"}, verde) {
		t.Error("dependencia ya verde debió pasar")
	}
	if depsVerdes([]string{"T-002"}, verde) {
		t.Error("dependencia pendiente no debió pasar")
	}
	if got := depsFaltantes([]string{"T-001", "T-003"}, verde); len(got) != 1 || got[0] != "T-003" {
		t.Errorf("depsFaltantes = %v, quiero [T-003]", got)
	}
}

// Una dependencia que quedó verde en una corrida anterior no debe
// bloquear a la que la necesita: antes `integrada` arrancaba vacío en
// cada corrida y la tarea se rechazaba con "no salió verde" para
// siempre.
func TestSembrarVerdesPrevias(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom", "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(root, state.State{ID: "T-001", Estado: state.Lista}); err != nil {
		t.Fatal(err)
	}
	if err := state.Save(root, state.State{ID: "T-002", Estado: state.Detenida}); err != nil {
		t.Fatal(err)
	}

	nueva := task.Task{ID: "T-009", Titulo: "T-009", DependeDe: []string{"T-001", "T-002"}}
	integrada := map[string]bool{}
	// sin repo git, RamaExiste es falso: la verde cuenta como ya entregada
	sembrarVerdesPrevias(context.Background(), root, []task.Task{nueva}, integrada)

	if !integrada["T-001"] {
		t.Error("T-001 quedó lista en una corrida anterior, debió contar como verde")
	}
	if integrada["T-002"] {
		t.Error("T-002 está detenida, no debió contar como verde")
	}
}

func TestAutoAgentes(t *testing.T) {
	casos := []struct {
		tareas int
		want   int
	}{
		{0, 1}, {1, 1}, {2, 2}, {8, 8}, {12, 12}, {16, 16}, {100, 16},
	}
	for _, c := range casos {
		if got := autoAgentes(c.tareas); got != c.want {
			t.Errorf("autoAgentes(%d) = %d, quiero %d", c.tareas, got, c.want)
		}
	}
}

func TestModeloParaTarea(t *testing.T) {
	cfg := config.Config{
		Estrategia: "equilibrada",
		Modelos:    map[string]string{"liviana": "glm-4", "media": "glm-5.2", "pesada": "claude-sonnet"},
		Proveedores: map[string]config.Proveedor{
			"ejecutor": {Modelo: "default-ejecutor"},
		},
	}

	// el flag gana sobre todo
	if got := modeloParaTarea(cfg, "flag-model", task.Task{Peso: "pesada"}); got != "flag-model" {
		t.Errorf("flag = %q, quiero flag-model", got)
	}
	// un constructor de peso pesada usa el modelo medio: el pesado es del arquitecto
	if got := modeloParaTarea(cfg, "", task.Task{Peso: "pesada"}); got != "glm-5.2" {
		t.Errorf("peso pesada = %q, quiero glm-5.2", got)
	}
	cfg.HeavyBuilder = "claude-sonnet"
	if got := modeloParaTarea(cfg, "", task.Task{Peso: "pesada"}); got != "claude-sonnet" {
		t.Errorf("heavy_builder = %q, quiero claude-sonnet", got)
	}
	cfg.HeavyBuilder = ""
	// sin peso, usa la estrategia
	if got := modeloParaTarea(cfg, "", task.Task{}); got != "glm-5.2" {
		t.Errorf("estrategia equilibrada = %q, quiero glm-5.2", got)
	}
	// peso sin modelo mapeado cae al ejecutor
	cfg2 := config.Config{Modelos: map[string]string{"media": "glm-5.2"}, Proveedores: map[string]config.Proveedor{"ejecutor": {Modelo: "default"}}}
	if got := modeloParaTarea(cfg2, "", task.Task{Peso: "liviana"}); got != "default" {
		t.Errorf("peso sin mapeo = %q, quiero default", got)
	}
}

func TestMotivoRechazoUneTodosLosChequeos(t *testing.T) {
	res := gate.Result{
		ID: "T-001",
		Chequeos: []gate.Check{
			{Nombre: "contrato válido", OK: true},
			{Nombre: "ejecutable", OK: false, Motivo: "falta listo_cuando · escribe el comando que dice \"ya está\""},
			{Nombre: "zonas prohibidas", OK: false, Motivo: "tocar_solo incluye go.sum"},
		},
	}
	got := motivoRechazo(res)
	if !strings.Contains(got, "ejecutable") || !strings.Contains(got, "falta listo_cuando") {
		t.Errorf("falta el primer chequeo fallido: %s", got)
	}
	if !strings.Contains(got, "zonas prohibidas") || !strings.Contains(got, "go.sum") {
		t.Errorf("falta el segundo chequeo fallido: %s", got)
	}
	if strings.Contains(got, "contrato válido") {
		t.Errorf("no debe incluir chequeos en verde: %s", got)
	}
}

func TestMotivoRechazoVacioCaeAPrimerMotivo(t *testing.T) {
	res := gate.Result{ID: "T-001"}
	if got := motivoRechazo(res); got != "" {
		t.Errorf("sin chequeos = %q, quiere vacío", got)
	}
}

func TestEmitirResultadosRechazadaImprimeMotivoCompleto(t *testing.T) {
	var b strings.Builder
	out = ui.New(&b, false)
	if err := emitirResultados([]runResult{{
		ID: "T-001", Titulo: "exportar", Estado: "rechazada",
		Motivo: "ejecutable · falta listo_cuando · zonas prohibidas · tocar_solo incluye go.sum",
	}}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "T-001") || !strings.Contains(got, "falta listo_cuando") || !strings.Contains(got, "go.sum") {
		t.Errorf("plain no imprimió el motivo completo:\n%s", got)
	}
}

func TestRechazarUsaHuerfano(t *testing.T) {
	productora := task.Task{ID: "T-001", Titulo: "wol", Expone: []string{"wol.Send(mac, addr string) error"}}
	buena := task.Task{ID: "T-002", Titulo: "api", Usa: []string{"wol.Send(mac, addr string) error"}}
	huerfana := task.Task{ID: "T-003", Titulo: "cli", Usa: []string{"config.Cargar(p string) error"}}

	todas := []task.Task{productora, buena, huerfana}
	ok, results := rechazarUsaHuerfano([]task.Task{buena, huerfana}, todas, nil)

	if len(ok) != 1 || ok[0].ID != "T-002" {
		t.Fatalf("aprobadas = %v, quiero solo T-002", ok)
	}
	if len(results) != 1 || results[0].ID != "T-003" || results[0].Estado != "rechazada" {
		t.Fatalf("results = %+v, quiero T-003 rechazada", results)
	}
}

func TestResolverAgenteTarea(t *testing.T) {
	cfg := config.Config{
		Agentes: map[string]config.Agente{
			"architect": {Provider: "claude", Modelo: "claude-sonnet", Skills: []string{"diseno"}},
			"ejecutor":  {Provider: "opencode", Modelo: "glm-5.2", Skills: []string{"refactor"}},
		},
		Proveedores: map[string]config.Proveedor{
			"planificador": {Modelo: "claude-haiku"},
		},
	}

	// 1. Tarea con agente architect
	tArch := task.Task{ID: "T-001", Agente: "architect"}
	ex, modelo, skills, _ := resolverAgenteTarea(cfg, nil, "", tArch)
	if ex != nil && ex.Name() != "claude" {
		t.Errorf("ex.Name() = %q, quiero claude", ex.Name())
	}
	if modelo != "claude-sonnet" {
		t.Errorf("modelo = %q, quiero claude-sonnet", modelo)
	}
	if len(skills) != 1 || skills[0] != "diseno" {
		t.Errorf("skills = %v, quiero [diseno]", skills)
	}

	// 2. Tarea sin agente (cae a ejecutor)
	tDefault := task.Task{ID: "T-002"}
	_, modeloDef, skillsDef, _ := resolverAgenteTarea(cfg, nil, "", tDefault)
	if modeloDef != "glm-5.2" {
		t.Errorf("modeloDef = %q, quiero glm-5.2", modeloDef)
	}
	if len(skillsDef) != 1 || skillsDef[0] != "refactor" {
		t.Errorf("skillsDef = %v, quiero [refactor]", skillsDef)
	}

	// 3. flagModelo tiene precedencia
	_, modeloFlag, _, _ := resolverAgenteTarea(cfg, nil, "modelo-manual", tArch)
	if modeloFlag != "modelo-manual" {
		t.Errorf("modeloFlag = %q, quiero modelo-manual", modeloFlag)
	}

	// 4. Tarea con agente en Proveedores
	tPlan := task.Task{ID: "T-003", Agente: "planificador"}
	_, modeloPlan, _, _ := resolverAgenteTarea(cfg, nil, "", tPlan)
	if modeloPlan != "claude-haiku" {
		t.Errorf("modeloPlan = %q, quiero claude-haiku", modeloPlan)
	}

	// 5. Tarea con arquetipo backend por defecto (sin declarar en config):
	// el arquetipo NO fija modelo, así que sin `modelos:` en config queda
	// vacío y el CLI usa el suyo. Inventar un id aquí era lo que mataba
	// cada corrida antes de gastar un token.
	tBack := task.Task{ID: "T-004", Agente: "backend"}
	_, modeloBack, skillsBack, skillPkgsBack := resolverAgenteTarea(config.Config{}, nil, "", tBack)
	if modeloBack != "" {
		t.Errorf("modeloBack = %q, quiero \"\" · el arquetipo no debe inventar un id de modelo", modeloBack)
	}

	// 5b. Con `modelos:` declarado, el peso de la tarea manda
	cfgPesos := config.Config{Modelos: map[string]string{"liviana": "chico", "media": "mediano", "pesada": "grande"}}
	_, modeloPesado, _, _ := resolverAgenteTarea(cfgPesos, nil, "", task.Task{ID: "T-005", Agente: "backend", Peso: "pesada"})
	if modeloPesado != "mediano" {
		t.Errorf("modeloPesado = %q, quiero mediano (el pesado es del arquitecto)", modeloPesado)
	}
	if len(skillsBack) == 0 {
		t.Error("skillsBack no debe estar vacío")
	}
	if len(skillPkgsBack) == 0 {
		t.Error("skillPkgsBack no debe estar vacío · el arquetipo backend trae la base + create-a-backend")
	}
}

// En un stack sin examinador ciego (node), las rutas de prueba dejan de
// estar vedadas: si nadie más las escribe, tiene que poder el
// implementador, o el listo_cuando apunta a un archivo que nunca existe.
func TestPatronesPruebaSegunElStack(t *testing.T) {
	cfg := config.Config{PatronesPrueba: []string{"*_test.go", "test/**"}}

	goRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(goRepo, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := patronesPrueba(cfg, goRepo); len(got) != 2 {
		t.Errorf("go = %v · con examinador las pruebas siguen vedadas", got)
	}

	nodeRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(nodeRepo, "package.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// vacío PERO NO nil: nil significa "config.yml no los declaró, usa los
	// del proyecto", y volvería a vedar rutas que nadie más escribe
	got := patronesPrueba(cfg, nodeRepo)
	if got == nil {
		t.Fatal("node = nil · nil se reinterpreta como \"usa los del proyecto\"")
	}
	if len(got) != 0 {
		t.Errorf("node = %v, quiero vacío · sin examinador nadie escribiría las pruebas", got)
	}

	// sin patrones en config, un stack con examinador cae en los del proyecto
	if got := patronesPrueba(config.Config{}, goRepo); len(got) == 0 {
		t.Error("go sin config debe usar los patrones por defecto")
	}
}

// La tarea de integración solo escribe pruebas: no expone nada, así que no
// hay examen ciego que proteger y la veda de rutas de prueba no le aplica.
// Con la veda del proyecto —como estaba— la esclusa la rechazaba por
// "tocar_solo incluye rutas de prueba" sin llegar a correr.
func TestVedaNoAplicaALaTareaDeIntegracion(t *testing.T) {
	goRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(goRepo, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{PatronesPrueba: []string{"*_test.go", "test/**"}}

	integracion := task.Task{
		Version: task.Version, ID: "T-004", Titulo: "prueba de integración",
		ListoCuando: "go test ./test/integracion/...",
		TocarSolo:   []string{"test/integracion/**"},
		Usa:         []string{"lexer.Tokenize(s string) []Token"},
	}
	if got := patronesPruebaTarea(cfg, goRepo, integracion); len(got) != 0 {
		t.Errorf("veda = %v · nadie más escribiría esa suite", got)
	}

	// y donde sí hay examen que proteger, la veda sigue intacta
	normal := task.Task{
		Version: task.Version, ID: "T-001", Titulo: "lexer",
		ListoCuando: "go test ./internal/lexer/...",
		TocarSolo:   []string{"internal/lexer/**"},
		Expone:      []string{"lexer.Tokenize(s string) []Token"},
	}
	if got := patronesPruebaTarea(cfg, goRepo, normal); len(got) != 2 {
		t.Errorf("veda = %v · una tarea examinable no toca sus pruebas", got)
	}
}

// Las skills del contrato mandan sobre las del rol: una tarea backend
// que el planificador marcó sin skills no recibe create-a-backend, y la
// que no declara nada sigue con las de su rol.
func TestSkillsDelContratoMandanSobreElRol(t *testing.T) {
	cfg := config.Config{}
	casos := []struct {
		skills []string
		quiero []string
	}{
		{nil, []string{"create-a-backend"}},
		{[]string{}, nil},
		{[]string{"clean-code"}, []string{"clean-code"}},
	}
	for _, c := range casos {
		_, _, _, got := resolverAgenteTarea(cfg, nil, "", task.Task{ID: "T-001", Agente: "backend", Skills: c.skills})
		if strings.Join(got, ",") != strings.Join(c.quiero, ",") {
			t.Errorf("skills %v → %v, quiero %v", c.skills, got, c.quiero)
		}
	}
}
