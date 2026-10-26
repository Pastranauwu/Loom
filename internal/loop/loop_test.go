package loop

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// agenteFalso simula un ejecutor: por defecto no hace nada; hacer puede
// escribir en el cuarto según la cantidad de invocaciones.
type agenteFalso struct {
	nombre string
	tokens Tokens
	veces  int
	hacer  func(veces int, req Request) (string, int, error)
}

func (a *agenteFalso) Name() string { return a.nombre }

func (a *agenteFalso) Run(_ context.Context, req Request) (Result, error) {
	a.veces++
	if a.hacer == nil {
		return Result{Tokens: a.tokens}, nil
	}
	stdout, code, err := a.hacer(a.veces, req)
	return Result{Stdout: stdout, ExitCode: code, Tokens: a.tokens}, err
}

func tareaDePrueba() task.Task {
	return task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         "exportar",
		ListoCuando:    "test -f src/done.txt",
		TocarSolo:      []string{"src/**"},
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}
}

func optsDePrueba(t *testing.T, root string, ag Agent, tk task.Task) Options {
	t.Helper()
	r, err := room.Create(context.Background(), root, tk.ID, "main")
	if err != nil {
		t.Fatalf("room.Create: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, tk.ID) })
	return Options{
		Agent:          ag,
		Root:           root,
		Room:           r,
		Task:           tk,
		Model:          "falso-1",
		Base:           "main",
		AgentTimeout:   5 * time.Second,
		PruebaTimeout:  5 * time.Second,
		PatronesPrueba: []string{"*_test.go", "test/**"},
	}
}

func TestRunVerdeEnSegundoIntento(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", tokens: Tokens{Entrada: 10, Salida: 5}}
	ag.hacer = func(veces int, req Request) (string, int, error) {
		if veces == 2 {
			escribir(t, req.RoomPath, "src/done.txt", "x\n")
		}
		return "", 0, nil
	}

	outcome, err := Run(context.Background(), optsDePrueba(t, root, ag, tareaDePrueba()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Verde || outcome.Intentos != 2 {
		t.Fatalf("Outcome = %+v, quiero verde en el intento 2", outcome)
	}

	attempts, err := ReadAttempts(root, "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, quiero 2", len(attempts))
	}
	if attempts[0].SalidaCodigo == nil || *attempts[0].SalidaCodigo != 1 {
		t.Errorf("intento 1 salida_codigo = %v, quiero 1", attempts[0].SalidaCodigo)
	}
	if attempts[1].SalidaCodigo == nil || *attempts[1].SalidaCodigo != 0 {
		t.Errorf("intento 2 salida_codigo = %v, quiero 0", attempts[1].SalidaCodigo)
	}
	if len(attempts[1].ArchivosTocados) != 1 || attempts[1].ArchivosTocados[0] != "src/done.txt" {
		t.Errorf("intento 2 archivos = %v", attempts[1].ArchivosTocados)
	}
	if attempts[1].LineasMas != 1 {
		t.Errorf("intento 2 lineas_mas = %d, quiero 1", attempts[1].LineasMas)
	}
	if attempts[0].Tokens.Entrada != 10 || attempts[1].Modelo != "falso-1" {
		t.Errorf("tokens/modelo mal anotados: %+v", attempts)
	}
}

// Escalar o retomar vuelve a llamar a Run sobre la misma tarea: el
// intento nuevo sigue la numeración y no pisa el log del anterior.
func TestRunNumeraIntentosTrasEscalar(t *testing.T) {
	root := repoConCommit(t)
	tk := tareaDePrueba()
	tk.ListoCuando = "false"
	tk.LimiteIntentos = 1
	opts := optsDePrueba(t, root, &agenteFalso{nombre: "falso"}, tk)
	for _, modelo := range []string{"falso-1", "falso-2"} {
		opts.Model = modelo
		if _, err := Run(context.Background(), opts); err != nil {
			t.Fatalf("Run %s: %v", modelo, err)
		}
	}
	attempts, err := ReadAttempts(root, "T-001")
	if err != nil || len(attempts) != 2 {
		t.Fatalf("attempts = %v, %v", attempts, err)
	}
	if attempts[1].Intento != 2 || attempts[1].Log != ".loom/runs/T-001/intento-2.log" {
		t.Errorf("el intento escalado no siguió la cuenta: %d %s", attempts[1].Intento, attempts[1].Log)
	}
	if b, err := os.ReadFile(LogPath(root, "T-001", 1)); err != nil || !strings.HasPrefix(string(b), "=== intento 1 ") {
		t.Errorf("se pisó intento-1.log: %v", err)
	}
}

func TestRunAgotaIntentos(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/progreso.txt", strings.Repeat("avance\n", n))
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "false"
	tk.LimiteIntentos = 2

	outcome, err := Run(context.Background(), optsDePrueba(t, root, ag, tk))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Verde {
		t.Fatal("una tarea que siempre falla salió verde")
	}
	if outcome.Intentos != 2 {
		t.Errorf("Intentos = %d, quiero 2", outcome.Intentos)
	}
	if !strings.Contains(outcome.Pregunta, "agotó 2 intentos") {
		t.Errorf("Pregunta = %q, quiero la pregunta concreta", outcome.Pregunta)
	}

	attempts, err := ReadAttempts(root, "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, quiero 2 (uno por intento)", len(attempts))
	}
}

func TestRunRevierteArchivosFueraDeAlcance(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	ag.hacer = func(veces int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/done.txt", "dentro\n")
		escribir(t, req.RoomPath, "docs/notas.txt", "fuera\n")
		return "", 0, nil
	}

	outcome, err := Run(context.Background(), optsDePrueba(t, root, ag, tareaDePrueba()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Verde {
		t.Fatalf("Outcome = %+v, quiero verde", outcome)
	}

	attempts, _ := ReadAttempts(root, "T-001")
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, quiero 1", len(attempts))
	}
	revertidos := attempts[0].RevertidosFueraDeAlcance
	if len(revertidos) != 1 || revertidos[0] != "docs/notas.txt" {
		t.Errorf("revertidos = %v, quiero [docs/notas.txt]", revertidos)
	}
	if _, err := os.Stat(filepath.Join(room.Dir(root), "T-001", "docs", "notas.txt")); !os.IsNotExist(err) {
		t.Error("el archivo fuera de alcance quedó en el cuarto")
	}
}

func TestAttemptsJSONListasVacias(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	tk := tareaDePrueba()
	tk.ListoCuando = "false"
	tk.LimiteIntentos = 1

	if _, err := Run(context.Background(), optsDePrueba(t, root, ag, tk)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(RunsDir(root), "T-001", "attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"archivos_tocados":[]`) {
		t.Errorf("archivos vacíos no serializan []: %s", s)
	}
	if !strings.Contains(s, `"revertidos_fuera_de_alcance":[]`) {
		t.Errorf("revertidos vacíos no serializan []: %s", s)
	}
	if !strings.Contains(s, `"simbolos_exportados":null`) {
		t.Errorf("símbolos sin soportar no serializan null: %s", s)
	}
	if !strings.Contains(s, `"tests_pasaron":null`) {
		t.Errorf("tests sin parsear no serializan null: %s", s)
	}
}

func TestPromptIncluyeContratoYErrorPrevio(t *testing.T) {
	tk := tareaDePrueba()
	p := promptPara(tk, nil, "", nil, "", "fallo de prueba", false)
	for _, want := range []string{"Tarea T-001", "Listo cuando: test -f src/done.txt", "Solo puedes tocar", "El intento anterior falló", "fallo de prueba"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sin %q:\n%s", want, p)
		}
	}
}

func TestPromptIncluyeSkills(t *testing.T) {
	tk := tareaDePrueba()
	p := promptPara(tk, nil, "", []string{"go", "refactor"}, "", "", false)
	want := "Habilidades de este rol: go, refactor"
	if !strings.Contains(p, want) {
		t.Errorf("prompt sin %q:\n%s", want, p)
	}
}

func TestPromptIncluyeContenidoDeSkills(t *testing.T) {
	tk := tareaDePrueba()
	p := promptPara(tk, nil, "", nil, "sé breve y directo", "", false)
	if !strings.Contains(p, "sé breve y directo") {
		t.Errorf("prompt sin el contenido de la skill:\n%s", p)
	}
}

// Sin examinador ciego (node, rust) la suite la escribe el propio agente:
// el prompt tiene que decírselo, o espera a un examinador que no existe y
// la tarea muere roja sin que nadie escriba las pruebas.
func TestPromptPruebasPropias(t *testing.T) {
	tk := tareaDePrueba()
	p := promptPara(tk, nil, "", nil, "", "", true)
	for _, want := range []string{"no tiene examinador ciego", "la escribes TÚ", "src/core/types.test.ts"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt con pruebas propias sin %q:\n%s", want, p)
		}
	}
	if got := promptPara(tk, nil, "", nil, "", "", false); strings.Contains(got, "no tiene examinador ciego") {
		t.Errorf("prompt sin pruebas propias no debe avisar:\n%s", got)
	}
}

// revisorFalso veto el primer intento verde y aprueba el segundo: es la
// forma de comprobar que un veto deja la tarea roja y que su veredicto
// llega al prompt siguiente como contexto.
type revisorFalso struct {
	vetos      map[int]bool // intento → vetar
	veces      int
	ultimoDiff string
}

func (r *revisorFalso) Revisar(_ context.Context, _ string, _ task.Task, diff string, intento int) (bool, string, Tokens) {
	r.veces++
	r.ultimoDiff = diff
	if r.vetos[intento] {
		return false, "falta el caso borde de la firma", Tokens{Entrada: 3, Salida: 2}
	}
	return true, "", Tokens{Entrada: 3, Salida: 2}
}

func TestRevisorVetaVerdeYElSiguienteArregla(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", tokens: Tokens{Entrada: 10, Salida: 5}}
	var prompts []string
	ag.hacer = func(veces int, req Request) (string, int, error) {
		prompts = append(prompts, req.Prompt)
		escribir(t, req.RoomPath, "src/done.txt", "x\n")
		return "", 0, nil
	}
	tk := tareaDePrueba()
	tk.LimiteIntentos = 3

	opts := optsDePrueba(t, root, ag, tk)
	opts.Revisor = &revisorFalso{vetos: map[int]bool{1: true}}

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Verde || outcome.Intentos != 2 {
		t.Fatalf("Outcome = %+v, quiero verde en el intento 2 tras el veto", outcome)
	}

	attempts, err := ReadAttempts(root, "T-001")
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 {
		t.Fatalf("attempts = %d, quiero 2", len(attempts))
	}
	if attempts[0].Revision == nil || attempts[0].Revision.Aprobada {
		t.Errorf("intento 1 debió quedar vetado: %+v", attempts[0].Revision)
	}
	if attempts[1].Revision == nil || !attempts[1].Revision.Aprobada {
		t.Errorf("intento 2 debió quedar aprobado: %+v", attempts[1].Revision)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "falta el caso borde") {
		t.Errorf("el veredicto del revisor no llegó al prompt siguiente:\n%v", prompts)
	}
}

func TestSinRevisorElVerdeEsVerde(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	ag.hacer = func(_ int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/done.txt", "x\n")
		return "", 0, nil
	}

	outcome, err := Run(context.Background(), optsDePrueba(t, root, ag, tareaDePrueba()))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Verde || outcome.Intentos != 1 {
		t.Fatalf("Outcome = %+v, quiero verde en 1 intento sin revisor", outcome)
	}
	attempts, _ := ReadAttempts(root, "T-001")
	if len(attempts) != 1 || attempts[0].Revision != nil {
		t.Errorf("sin revisor no debe haber revision: %+v", attempts[0])
	}
}

// contadorPrueba es el mínimo que loop pide al presupuesto: Gastar.
type contadorPrueba struct {
	limite, usado int
}

func (c *contadorPrueba) Gastar(n int) bool {
	if c.limite <= 0 {
		return true
	}
	if c.usado+n > c.limite {
		return false
	}
	c.usado += n
	return true
}

// ventanasPrueba simula el ledger: registra el gasto y, si rechaza, el
// bucle debe cortar con el motivo de ventana.
type ventanasPrueba struct {
	registrado int
	rechaza    bool
}

func (v *ventanasPrueba) Registrar(_ string, n int) bool {
	v.registrado += n
	return !v.rechaza
}

func TestRunCortaPorVentana(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "claude", tokens: Tokens{Entrada: 10, Salida: 5}}
	tk := tareaDePrueba()
	tk.ListoCuando = "false"
	tk.LimiteIntentos = 3

	o := optsDePrueba(t, root, ag, tk)
	o.Proveedor = "claude"
	v := &ventanasPrueba{rechaza: true}
	o.Ventanas = v

	outcome, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Verde {
		t.Fatal("una tarea roja no puede quedar verde")
	}
	if !strings.Contains(outcome.Pregunta, MotivoVentanas) {
		t.Errorf("Pregunta = %q, quiero el motivo de ventana", outcome.Pregunta)
	}
	if v.registrado != 15 {
		t.Errorf("el ledger registró %d, quiero 15 (el primer intento)", v.registrado)
	}
	if outcome.Intentos != 1 {
		t.Errorf("Intentos = %d, quiero 1 (se cortó al primer rechazo)", outcome.Intentos)
	}
}

func TestRunRegistraVentanasTambienCuandoEsVerde(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "opencode", tokens: Tokens{Entrada: 10, Salida: 5}}
	ag.hacer = func(veces int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/done.txt", "x\n")
		return "", 0, nil
	}

	o := optsDePrueba(t, root, ag, tareaDePrueba())
	o.Proveedor = "opencode"
	v := &ventanasPrueba{}
	o.Ventanas = v

	outcome, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.Verde {
		t.Fatalf("Outcome = %+v, quiero verde", outcome)
	}
	if v.registrado != 15 {
		t.Errorf("el ledger registró %d, quiero 15 aunque fuera verde", v.registrado)
	}
}

func TestRunCortaPorPresupuesto(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", tokens: Tokens{Entrada: 10, Salida: 5}}
	tk := tareaDePrueba()
	tk.ListoCuando = "false"
	tk.LimiteIntentos = 3

	o := optsDePrueba(t, root, ag, tk)
	o.Presupuesto = &contadorPrueba{limite: 20}

	outcome, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Verde {
		t.Fatal("una tarea roja no puede quedar verde")
	}
	if !strings.Contains(outcome.Pregunta, MotivoPresupuesto) {
		t.Errorf("Pregunta = %q, quiero el motivo de presupuesto", outcome.Pregunta)
	}
	// cada intento gasta 15; con tope 20 el segundo ya no cabe y se corta
	// en lugar de quemar los tres intentos
	if outcome.Intentos != 2 {
		t.Errorf("Intentos = %d, quiero 2 (el segundo pasó el tope)", outcome.Intentos)
	}
	attempts, _ := ReadAttempts(root, "T-001")
	if len(attempts) != 2 {
		t.Errorf("attempts = %d, quiero 2 (se cortó antes de quemar el tercero)", len(attempts))
	}
}

// el presupuesto se hacía cumplir al entregar sin que el agente supiera
// nunca que existía: lo estimaba el planificador y lo cobraba ship.
func TestPromptLlevaElPresupuesto(t *testing.T) {
	tk := task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         "exportar clientes",
		ListoCuando:    "true",
		LimiteIntentos: 3,
		LimiteLineas:   250,
	}
	p := promptPara(tk, nil, "", nil, "", "", false)
	if !strings.Contains(p, "250") {
		t.Errorf("el prompt del agente debe declarar el presupuesto:\n%s", p)
	}
}

func TestRunCortaFalloRepetidoSinCambios(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	tk := tareaDePrueba()
	tk.ListoCuando = "echo contrato-incompatible; exit 1"
	tk.LimiteIntentos = 8
	out, err := Run(context.Background(), optsDePrueba(t, root, ag, tk))
	if err != nil {
		t.Fatal(err)
	}
	if out.Verde || ag.veces != 2 || !strings.Contains(out.Pregunta, "sin progreso") {
		t.Fatalf("%+v, llamadas=%d", out, ag.veces)
	}
	as, err := ReadAttempts(root, tk.ID)
	if err != nil || len(as) != 2 {
		t.Fatalf("intentos=%d, error=%v", len(as), err)
	}
}

func TestRunContinuaConCambiosAunqueFalloSeaIgual(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/progreso.txt", strings.Repeat("avance\n", n))
		if n == 3 {
			escribir(t, req.RoomPath, "src/done.txt", "listo")
		}
		return "", 0, nil
	}}
	out, err := Run(context.Background(), optsDePrueba(t, root, ag, tareaDePrueba()))
	if err != nil || !out.Verde || ag.veces != 3 {
		t.Fatalf("%+v, error=%v, llamadas=%d", out, err, ag.veces)
	}
}

// el agente reescribía cada vez la prueba que la reversión
// quitaba; el intento no dejaba nada y la escalera pagaba qwen-max.
func TestRunSinProgresoPorReversionNoEscala(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "fuera/x.txt", "otra vez")
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "exit 1"
	tk.LimiteIntentos = 5
	out, err := Run(context.Background(), optsDePrueba(t, root, ag, tk))
	if err != nil {
		t.Fatal(err)
	}
	if ag.veces != 2 || !out.NoEscalar || !strings.Contains(out.Pregunta, "fuera/x.txt") {
		t.Fatalf("%+v, llamadas=%d", out, ag.veces)
	}
}

func TestRunTopePorTareaSumaLlamadas(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/progreso.txt", strings.Repeat("avance\n", n))
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "exit 1"
	tk.LimiteIntentos = 2
	opts := optsDePrueba(t, root, ag, tk)
	for i := 0; i < 3; i++ {
		if _, err := Run(context.Background(), opts); err != nil {
			t.Fatal(err)
		}
	}
	as, _ := ReadAttempts(root, tk.ID)
	// el agente falso no reporta tokens: ningún intento cuenta como
	// pagado y el tope no corta
	if len(as) != 6 {
		t.Fatalf("intentos=%d", len(as))
	}
	if n := intentosPagados(as, time.Time{}, ""); n != 0 {
		t.Fatalf("pagados=%d", n)
	}
	for i := range as {
		as[i].Tokens.Entrada = 1
	}
	if n := intentosPagados(as, as[3].Inicio, ""); n != 3 {
		t.Fatalf("desde el cuarto: pagados=%d", n)
	}
	// los intentos contra otra base no gastan el tope de esta; los que
	// no anotaron base (anteriores a que se guardara) siguen contando
	if as[0].Base == "" {
		t.Fatal("el intento no anotó el árbol de su base")
	}
	as[0].Base, as[1].Base, as[2].Base = "otra", "otra", ""
	if n := intentosPagados(as, time.Time{}, as[3].Base); n != 4 {
		t.Fatalf("contra la base actual: pagados=%d, quiero 4", n)
	}
}

func TestPruebaNoCorrio(t *testing.T) {
	uno, cuatro, cero, ciento := 1, 4, 0, 127
	casos := []struct {
		cmd    string
		code   *int
		salida string
		quiero bool
	}{
		{"pytest backend/tests/test_base.py", &cuatro, "ERROR: file or directory not found", true},
		{"npm test -- x.test.tsx", &uno, "No test files found, exiting with code 1", true},
		{"go test ./nada/...", &uno, "go: warning: \"./nada/...\" matched no packages", true},
		{"foo", &ciento, "sh: foo: not found", true},
		{"pytest", &uno, "1 failed, 2 passed", false},
		{"go test ./...", &uno, "--- FAIL: TestX", false},
		{"pytest", &cero, "", false},
		{"pytest", nil, "", true},
	}
	for _, c := range casos {
		if got := PruebaNoCorrio(c.cmd, c.code, c.salida); got != c.quiero {
			t.Errorf("%q %q = %v", c.cmd, c.salida, got)
		}
	}
}

type examinadorContado struct{ veces int }

func (e *examinadorContado) Run(context.Context, string) (bool, error) { e.veces++; return true, nil }

// la prueba del esqueleto ya está en el commit con que arranca la tarea:
// es el oráculo, el agente barato no la puede aflojar ni se examina encima
func TestRunProtegeLaPruebaQueYaExistia(t *testing.T) {
	root := repoConCommit(t)
	escribir(t, root, "src/suma.test.js", "original\n")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "esqueleto")
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/suma.test.js", "aflojada\n")
		escribir(t, req.RoomPath, "src/suma.js", strings.Repeat("x\n", n))
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "grep -q aflojada src/suma.test.js"
	tk.LimiteIntentos = 1
	opts := optsDePrueba(t, root, ag, tk)
	opts.PatronesPrueba = []string{}
	ex := &examinadorContado{}
	opts.Examinador = ex
	out, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if out.Verde || ex.veces != 0 {
		t.Fatalf("verde=%v examinador=%d", out.Verde, ex.veces)
	}
	as, _ := ReadAttempts(root, tk.ID)
	if len(as) != 1 || strings.Join(as[0].RevertidosFueraDeAlcance, ",") != "src/suma.test.js" {
		t.Fatalf("%+v", as)
	}
}

// visualExigente nunca queda conforme: comprueba que su crítica llegue al
// intento siguiente y que el tope corte el gusto del modelo
type visualExigente struct{ veces int }

func (v *visualExigente) Revisar(context.Context, room.Room, task.Task, int) (bool, string, Tokens) {
	v.veces++
	return false, "el vidrio no se nota: el fondo es plano", Tokens{Entrada: 4, Salida: 2}
}

func TestRevisionVisualDevuelveHastaElTope(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso", tokens: Tokens{Entrada: 10, Salida: 5}}
	var prompts []string
	ag.hacer = func(veces int, req Request) (string, int, error) {
		prompts = append(prompts, req.Prompt)
		escribir(t, req.RoomPath, "src/done.txt", fmt.Sprintf("%d\n", veces))
		return "", 0, nil
	}
	tk := tareaDePrueba()
	tk.LimiteIntentos = 5
	tk.TocarSolo = append(tk.TocarSolo, "src/Pagina.tsx")
	opts := optsDePrueba(t, root, ag, tk)
	v := &visualExigente{}
	opts.RevisorVisual = v

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Verde || outcome.Intentos != TopeVisual+1 || v.veces != TopeVisual {
		t.Fatalf("outcome = %+v, revisiones = %d; quiero verde tras %d críticas", outcome, v.veces, TopeVisual)
	}
	if !strings.Contains(prompts[1], "el vidrio no se nota") {
		t.Errorf("la crítica visual no llegó al intento siguiente:\n%s", prompts[1])
	}
}

// una tarea que no toca interfaz no gasta revisión visual
func TestSinInterfazNoHayRevisionVisual(t *testing.T) {
	root := repoConCommit(t)
	ag := &agenteFalso{nombre: "falso"}
	ag.hacer = func(_ int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/done.txt", "x\n")
		return "", 0, nil
	}
	opts := optsDePrueba(t, root, ag, tareaDePrueba())
	v := &visualExigente{}
	opts.RevisorVisual = v
	if outcome, err := Run(context.Background(), opts); err != nil || !outcome.Verde || v.veces != 0 {
		t.Fatalf("outcome = %+v err = %v revisiones = %d", outcome, err, v.veces)
	}
}

func TestAlcanceParaListaLosArchivosDelCuarto(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if alcancePara(dir) != "" {
		t.Error("un cuarto vacío no tiene nada que listar")
	}
	if err := os.WriteFile(filepath.Join(dir, "a.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := alcancePara(dir); !strings.Contains(p, "\na.ts\n") {
		t.Errorf("falta el archivo en la lista:\n%s", p)
	}
}

// Si el contrato nombra la prueba vieja en tocar_solo, quien planeó
// decidió que el cambio la deja obsoleta: el agente la puede actualizar.
func TestRunDejaActualizarLaPruebaViejaQueElContratoNombra(t *testing.T) {
	root := repoConCommit(t)
	escribir(t, root, "src/suma.test.js", "original\n")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "base")
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		escribir(t, req.RoomPath, "src/suma.test.js", "actualizada\n")
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "grep -q actualizada src/suma.test.js"
	tk.TocarSolo = []string{"src/suma.js", "src/suma.test.js"}
	tk.LimiteIntentos = 1
	opts := optsDePrueba(t, root, ag, tk)
	opts.PatronesPrueba = []string{}
	out, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	as, _ := ReadAttempts(root, tk.ID)
	if !out.Verde || len(as) != 1 || len(as[0].RevertidosFueraDeAlcance) != 0 {
		t.Fatalf("verde=%v intentos=%+v", out.Verde, as)
	}
}

// vitest y jest ignoran el archivo que falta y corren el resto: verde sin
// la prueba de la tarea no es verde.
func TestRunNoEsVerdeSinLaPruebaQueListoCuandoNombra(t *testing.T) {
	root := repoConCommit(t)
	var prompts []string
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		prompts = append(prompts, req.Prompt)
		escribir(t, req.RoomPath, "src/done.txt", "x\n")
		if n > 1 {
			escribir(t, req.RoomPath, "src/nuevo.test.js", "prueba\n")
		}
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.ListoCuando = "test -f src/done.txt && true src/nuevo.test.js"
	tk.LimiteIntentos = 2
	opts := optsDePrueba(t, root, ag, tk)
	opts.PatronesPrueba = []string{}
	out, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Verde || out.Intentos != 2 {
		t.Fatalf("el primer intento no escribió la prueba y no puede ser verde: %+v", out)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "src/nuevo.test.js no existe") {
		t.Errorf("el segundo intento debe saber qué faltó:\n%v", prompts)
	}
}

// El agente escribe código y prueba. Si la prueba pasa igual con el
// código de antes de la tarea, no prueba el cambio: el intento es rojo.
func TestRunRechazaLaPruebaQuePasaSinLaImplementacion(t *testing.T) {
	root := repoConCommit(t)
	var prompts []string
	ag := &agenteFalso{nombre: "falso", hacer: func(n int, req Request) (string, int, error) {
		prompts = append(prompts, req.Prompt)
		escribir(t, req.RoomPath, "src/suma.js", "hecho\n")
		// primer intento: una prueba que no mira el código; segundo: sí
		escribir(t, req.RoomPath, "src/suma.test.js", "ok\n")
		return "", 0, nil
	}}
	tk := tareaDePrueba()
	tk.LimiteIntentos = 2
	tk.ListoCuando = "grep -q ok src/suma.test.js"
	opts := optsDePrueba(t, root, ag, tk)
	opts.PatronesPrueba = []string{}
	out, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if out.Verde {
		t.Fatal("una prueba que pasa sin la implementación no puede dar verde")
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "pasa igual SIN tu implementación") {
		t.Errorf("el siguiente intento debe saber por qué: %v", len(prompts))
	}
	// el cuarto queda con el trabajo del agente, no con el código de antes
	if b, err := os.ReadFile(filepath.Join(opts.Room.Path, "src/suma.js")); err != nil || string(b) != "hecho\n" {
		t.Errorf("la comprobación debe restaurar el cuarto: %q %v", b, err)
	}

	// la misma tarea con una prueba que sí depende del código es verde
	root2 := repoConCommit(t)
	tk.ListoCuando = "grep -q ok src/suma.test.js && grep -q hecho src/suma.js"
	opts = optsDePrueba(t, root2, ag, tk)
	opts.PatronesPrueba = []string{}
	if out, err := Run(context.Background(), opts); err != nil || !out.Verde || out.Intentos != 1 {
		t.Fatalf("una prueba que falla sin el código es legítima: %+v %v", out, err)
	}
}
