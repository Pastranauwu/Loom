package main

import (
	"context"
	"github.com/Pastranauwu/loom/internal/executor"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/task"
	"github.com/Pastranauwu/loom/internal/ui"
)

func TestSanearAlcance(t *testing.T) {
	out = ui.New(io.Discard, false) // sin printer, sanearAlcance revienta al avisar
	zonas, patrones := zonasYPatrones(config.Config{})
	bs := []plan.Borrador{
		{Titulo: "init go", TocarSolo: []string{"go.mod", "go.sum", "Makefile"}},
		{Titulo: "wol", TocarSolo: []string{"internal/wol/**"}},
	}
	sanearAlcance(bs, zonas, patrones, nil)

	if got := strings.Join(bs[0].TocarSolo, ","); got != "go.mod,Makefile" {
		t.Errorf("tocar_solo[0] = %q, quiero go.mod,Makefile", got)
	}
	if got := strings.Join(bs[1].TocarSolo, ","); got != "internal/wol/**" {
		t.Errorf("tocar_solo[1] = %q, sin cambios", got)
	}
}

// Sin examinador ciego el archivo de prueba que listo_cuando va a correr
// tiene que entrar en tocar_solo, o la reversión de alcance se lo quita
// al agente y la tarea queda roja para siempre quemando los intentos.
func TestAmpliarPruebasPropias(t *testing.T) {
	out = ui.New(io.Discard, false)
	bs := []plan.Borrador{
		{
			Titulo:      "definir tipos",
			ListoCuando: "npx vitest run src/core/types",
			TocarSolo:   []string{"src/core/types.ts"},
		},
		{
			Titulo:      "almacenar",
			ListoCuando: "node --test test/validator.test.js",
			TocarSolo:   []string{"src/store.js"},
		},
		{
			Titulo:      "módulo puro",
			ListoCuando: "go test ./internal/wol/...",
			TocarSolo:   []string{"internal/wol/**"},
		},
	}
	ampliarPruebasPropias(bs)

	if !config.MatchesAny(bs[0].TocarSolo, "src/core/types.test.ts") {
		t.Errorf("vitest sin archivo de prueba: tocar_solo[0] = %v, quiero src/core/types.test.ts", bs[0].TocarSolo)
	}
	if !config.MatchesAny(bs[1].TocarSolo, "test/validator.test.js") {
		t.Errorf("test explícito: tocar_solo[1] = %v, quiero test/validator.test.js", bs[1].TocarSolo)
	}
	// Go sí tiene examinador ciego: nada que ampliar, y el glob ya cubre
	// la suite del paquete.
	if len(bs[2].TocarSolo) != 1 {
		t.Errorf("go no debe ampliarse: tocar_solo[2] = %v", bs[2].TocarSolo)
	}
}

func TestIdsCorrelativos(t *testing.T) {
	root := t.TempDir()
	dir := config.TasksDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := task.Save(dir, task.Task{Version: task.Version, ID: "T-001", Titulo: "x", ListoCuando: "true", LimiteIntentos: 3, LimiteLineas: 200}); err != nil {
		t.Fatal(err)
	}
	ids, err := idsCorrelativos(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"T-002", "T-003", "T-004"}
	for i, w := range want {
		if ids[i] != w {
			t.Errorf("ids[%d] = %s, quiero %s", i, ids[i], w)
		}
	}
}

func TestIdsCorrelativosVacio(t *testing.T) {
	dir := config.TasksDir(t.TempDir())
	ids, err := idsCorrelativos(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != "T-001" {
		t.Errorf("primer id = %s, quiero T-001", ids[0])
	}
}

// con T-001 y T-002 ya en el repo, el plan arranca en T-003: su "T-001"
// es la T-003 real, no la vieja
func TestTraducirDependencias(t *testing.T) {
	bs := []plan.Borrador{
		{Titulo: "base"},
		{Titulo: "mac", DependeDe: []string{"T-001"}},
		{Titulo: "wol", DependeDe: []string{"T-001", "2"}},
		{Titulo: "ajena", DependeDe: []string{"T-099"}},
	}
	traducirDependencias(bs, []string{"T-003", "T-004", "T-005", "T-006"}, nil)
	got := [][]string{bs[1].DependeDe, bs[2].DependeDe, bs[3].DependeDe}
	want := [][]string{{"T-003"}, {"T-003", "T-004"}, {"T-099"}}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Errorf("tarea %d: depende_de = %v, quiero %v", i+1, got[i], want[i])
		}
	}
}

// T-001 ya existe, el plan recibe T-019.. y el
// modelo usa esos ids reales. Leerlos por posición armaba ciclos.
func TestTraducirDependenciasRespetaIdsRealesYPrevios(t *testing.T) {
	bs := []plan.Borrador{
		{Titulo: "reglas", DependeDe: []string{"T-001"}},
		{Titulo: "calificador", DependeDe: []string{"T-019", "T-001"}},
	}
	traducirDependencias(bs, []string{"T-019", "T-020"}, map[string]bool{"T-001": true})
	if got := strings.Join(bs[0].DependeDe, ","); got != "T-001" {
		t.Errorf("reglas depende_de = %s, quiero T-001", got)
	}
	if got := strings.Join(bs[1].DependeDe, ","); got != "T-019,T-001" {
		t.Errorf("calificador depende_de = %s, quiero T-019,T-001", got)
	}
}

func TestConfirmar(t *testing.T) {
	if !confirmar(strings.NewReader("s\n")) {
		t.Error("s debió confirmar")
	}
	if confirmar(strings.NewReader("n\n")) {
		t.Error("n no debió confirmar")
	}
	if confirmar(strings.NewReader("q\n")) {
		t.Error("respuesta suelta no debió confirmar")
	}
}

// El "como" del planificador es la instrucción que recibe el ejecutor:
// si se pierde al crear el contrato, el agente barato corre sin la
// orientación del modelo grande. Debe llegar a Notas (mismo camino que
// usa la recursión, recurse.replanDesdeContrato) y de ahí al prompt.
func TestComoLlegaAlContrato(t *testing.T) {
	b := plan.Borrador{
		Titulo:      "exportar clientes",
		ListoCuando: "go test ./internal/export/...",
		TocarSolo:   []string{"internal/export/**"},
		Como:        "empieza por el encoder, no toques el handler",
	}
	tk := task.Task{
		Version: task.Version, ID: "T-001", Titulo: b.Titulo,
		ListoCuando: b.ListoCuando, TocarSolo: b.TocarSolo,
		Notas: b.Como, LimiteIntentos: 3, LimiteLineas: 200,
	}
	leida, err := task.Parse(tk.Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if leida.Notas != b.Como {
		t.Errorf("notas = %q, quiero %q · el como se perdió en el contrato", leida.Notas, b.Como)
	}
}

func TestCerrarDependencias(t *testing.T) {
	props := []propuesta{
		{ID: "T-001"},
		{ID: "T-002", DependeDe: []string{"T-001"}},
		{ID: "T-003", DependeDe: []string{"T-002"}},
		{ID: "T-004"},
	}

	// descartar T-001 arrastra a T-002 y, en cascada, a T-003
	elegidas := map[string]bool{"T-002": true, "T-003": true, "T-004": true}
	arrastradas := cerrarDependencias(props, elegidas)
	if len(arrastradas) != 2 {
		t.Fatalf("arrastradas = %v, quiero T-002 y T-003", arrastradas)
	}
	if len(elegidas) != 1 || !elegidas["T-004"] {
		t.Errorf("elegidas = %v, solo T-004 sobrevive", elegidas)
	}
}

func TestCerrarDependenciasNoTocaLoCoherente(t *testing.T) {
	props := []propuesta{{ID: "T-001"}, {ID: "T-002", DependeDe: []string{"T-001"}}}
	elegidas := map[string]bool{"T-001": true, "T-002": true}
	if arrastradas := cerrarDependencias(props, elegidas); len(arrastradas) != 0 {
		t.Errorf("arrastradas = %v, quiero ninguna", arrastradas)
	}
	if len(elegidas) != 2 {
		t.Errorf("elegidas = %v", elegidas)
	}
}

// Una dependencia que no está en el plan ya vive en el repo: no arrastra.
func TestCerrarDependenciasIgnoraLasDeFueraDelPlan(t *testing.T) {
	props := []propuesta{{ID: "T-009", DependeDe: []string{"T-001"}}}
	elegidas := map[string]bool{"T-009": true}
	if arrastradas := cerrarDependencias(props, elegidas); len(arrastradas) != 0 {
		t.Errorf("arrastradas = %v · T-001 no es parte de este plan", arrastradas)
	}
}

type ejecutorPlanCaptura struct{ req executor.Request }

func (e *ejecutorPlanCaptura) Name() string                             { return "claude" }
func (e *ejecutorPlanCaptura) Available() error                         { return nil }
func (e *ejecutorPlanCaptura) Models(context.Context) ([]string, error) { return nil, nil }
func (e *ejecutorPlanCaptura) Run(_ context.Context, r executor.Request) (executor.Result, error) {
	e.req = r
	return executor.Result{Text: "plan"}, nil
}
func TestGeneradorRespetaTimeoutConfigurado(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(config.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{TimeoutAgente: 900}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	ex := &ejecutorPlanCaptura{}
	g := generadorPlan{ex: ex, root: root, modelo: "opus", effort: "medium"}
	if _, err := g.Generar(context.Background(), "diseña snake"); err != nil {
		t.Fatal(err)
	}
	if ex.req.Timeout != 15*time.Minute || ex.req.Effort != "medium" || ex.req.Model != "opus" {
		t.Fatalf("request: %+v", ex.req)
	}
}

type generadorContado struct{ n *int }

func (g generadorContado) Generar(context.Context, string) (string, error) {
	*g.n++
	return "plan", nil
}

// un plan pagado que falló después (parseo, validación) se recompone
// con el mismo texto: el modelo no se vuelve a llamar mientras el
// prompt no cambie
func TestPlanGuardadoReusaMismoPrompt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(config.Dir(root), 0755); err != nil {
		t.Fatal(err)
	}
	n := 0
	g := planGuardado{generadorContado{&n}, root, ""}
	for _, p := range []string{"diseña snake", "diseña snake", "diseña tetris"} {
		if texto, err := g.Generar(context.Background(), p); err != nil || texto != "plan" {
			t.Fatalf("Generar(%q) = %q, %v", p, texto, err)
		}
	}
	if n != 2 {
		t.Errorf("llamadas al modelo = %d, quiero 2", n)
	}
}

// un arquitecto escribió depende_de: [2, 5] con un plan que
// numera desde T-002: son ids, y leídos como posición armaban un ciclo
func TestDependenciasNumericasSonIdsSiExisten(t *testing.T) {
	ids := []string{"T-002", "T-003", "T-004", "T-005"}
	got := dependenciasDelModelo([]string{"2", "5", "T-003"}, ids, nil)
	if strings.Join(got, ",") != "T-002,T-005,T-003" {
		t.Fatalf("%v", got)
	}
	// sin id que coincida, sigue siendo posición (plan que numera desde 1
	// cuando ya hay tareas previas)
	got = dependenciasDelModelo([]string{"1"}, []string{"T-019", "T-020"}, nil)
	if got[0] != "T-019" {
		t.Fatalf("%v", got)
	}
}

// vitest verde y tsc con 7 errores. El relleno tiene que pasar
// también el build, y el código de salida sigue siendo el de la prueba
// aunque traiga ";" o "||"
func TestConVerificarAnteponeElBuild(t *testing.T) {
	if got := conVerificar("bash scripts/verify.sh", "cd frontend && npx vitest run a.test.ts"); got != "bash scripts/verify.sh && (cd frontend && npx vitest run a.test.ts)" {
		t.Fatalf("%q", got)
	}
	if got := conVerificar("", "go test ./x"); got != "go test ./x" {
		t.Fatalf("sin verificar: %q", got)
	}
}

// la tarea final "depende de todas" se incluyó a sí misma y el
// plan ya pagado murió en ValidatePlan con "dependencia circular"
func TestTraducirDependenciasQuitaLaAutodependencia(t *testing.T) {
	ids := []string{"T-056", "T-057", "T-058"}
	bs := []plan.Borrador{{}, {DependeDe: []string{"T-056"}}, {DependeDe: []string{"T-056", "T-057", "T-058"}}}
	traducirDependencias(bs, ids, nil)
	if strings.Join(bs[2].DependeDe, ",") != "T-056,T-057" {
		t.Fatalf("%v", bs[2].DependeDe)
	}
	if ciclo(bs, ids) != nil {
		t.Fatal("sin la autodependencia no hay ciclo")
	}
	bs[0].DependeDe = []string{"T-058"}
	if c := ciclo(bs, ids); len(c) == 0 {
		t.Fatal("T-056 → T-058 → T-056 es un ciclo")
	}
}

func TestDNAAddRmYConstitucion(t *testing.T) {
	root := t.TempDir()
	out = ui.New(io.Discard, false)
	if err := os.MkdirAll(config.Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := (config.Config{Base: "main"}).Save(root); err != nil {
		t.Fatal(err)
	}
	if err := runDNA(root, []string{"add", "hexagonal"}); err != nil {
		t.Fatal(err)
	}
	if err := runDNA(root, []string{"add", "inventado"}); err == nil {
		t.Error("un dna desconocido debe fallar")
	}
	c, err := constitucionDe(root)
	if err != nil || !strings.Contains(c, "hexagonal") {
		t.Fatalf("constitución = %q, %v", c, err)
	}
	if err := runDNA(root, []string{"rm", "hexagonal"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := constitucionDe(root); strings.Contains(c, "hexagonal") {
		t.Error("rm debe quitarlo")
	}
}
