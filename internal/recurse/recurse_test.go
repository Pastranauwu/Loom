package recurse

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repoConCommit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	return root
}

// generadorFalso simula al planificador: devuelve el texto fijo que se le
// dio, ignorando el prompt real.
type generadorFalso struct{ texto string }

func (g generadorFalso) Generar(_ context.Context, _ string) (string, error) { return g.texto, nil }

// ejecutorFalso simula al agente hoja: escribe un archivo fijo en el
// cuarto que se le dio, para que el listo_cuando de la subtarea pase.
type ejecutorFalso struct{}

func (ejecutorFalso) Name() string { return "falso" }

func (ejecutorFalso) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	abs := filepath.Join(req.RoomPath, "src", "done.txt")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return loop.Result{}, err
	}
	if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
		return loop.Result{}, err
	}
	return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
}

func tareaRecursivaDePrueba() task.Task {
	return task.Task{
		Version:         task.Version,
		ID:              "T-100",
		Titulo:          "feature grande",
		ListoCuando:     "test -f src/done.txt",
		TocarSolo:       []string{"src/**"},
		LimiteIntentos:  3,
		LimiteLineas:    200,
		Recursivo:       true,
		LimiteSubtareas: 2,
	}
}

func TestAgentRunDescomponeYIntegra(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[{"titulo": "crear done.txt", "listo_cuando": "test -f src/done.txt", "tocar_solo": ["src/**"]}]`
	a := Agent{
		Planificador: generadorFalso{texto: texto},
		Ejecutor:     ejecutorFalso{},
		Task:         parentTask,
	}

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}
	if res.Tokens.Entrada != 1 || res.Tokens.Salida != 1 {
		t.Errorf("tokens = %+v, quiero los de la subtarea", res.Tokens)
	}

	if _, err := os.Stat(filepath.Join(parentRoom.Path, "src", "done.txt")); err != nil {
		t.Fatalf("src/done.txt no se integró al cuarto padre: %v", err)
	}
}

func TestAgentRunRechazaDemasiadasSubtareas(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentTask.LimiteSubtareas = 1
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[
		{"titulo": "uno", "listo_cuando": "test -f src/a.txt", "tocar_solo": ["src/**"]},
		{"titulo": "dos", "listo_cuando": "test -f src/b.txt", "tocar_solo": ["src/**"]}
	]`
	a := Agent{
		Planificador: generadorFalso{texto: texto},
		Ejecutor:     ejecutorFalso{},
		Task:         parentTask,
	}

	if _, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path}); err == nil {
		t.Fatal("Agent.Run debió rechazar 2 subtareas con límite 1")
	}
}

func TestRestringirAlcanceRecortaFueraDePermitido(t *testing.T) {
	bs := []plan.Borrador{
		{Titulo: "a", TocarSolo: []string{"src/foo/**", "otro/lugar/**"}},
	}
	restringirAlcance(bs, []string{"src/**"})
	if len(bs[0].TocarSolo) != 1 || bs[0].TocarSolo[0] != "src/foo/**" {
		t.Errorf("TocarSolo = %v, quiero solo [src/foo/**]", bs[0].TocarSolo)
	}
}

func TestDentroDeAlcance(t *testing.T) {
	permitidos := []string{"src/**"}
	if !dentroDeAlcance("src/foo/**", permitidos) {
		t.Error("src/foo/** debió quedar dentro de src/**")
	}
	if dentroDeAlcance("otro/**", permitidos) {
		t.Error("otro/** no debió quedar dentro de src/**")
	}
}

// generadorSecuencial devuelve cada texto en orden, para simulaciones de
// dos llamadas: la descomposición y luego el dictamen del supervisor.
type generadorSecuencial struct{ textos []string }

func (g *generadorSecuencial) Generar(_ context.Context, _ string) (string, error) {
	if len(g.textos) == 0 {
		return "", errors.New("sin textos")
	}
	t := g.textos[0]
	g.textos = g.textos[1:]
	return t, nil
}

// ejecutorPorModelo deja la subtarea verde solo con el modelo pesado:
// con cualquier otro escribe trabajo que no satisface el listo_cuando.
type ejecutorPorModelo struct{}

func (ejecutorPorModelo) Name() string { return "por-modelo" }

func (ejecutorPorModelo) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	nombre := "wrong.txt"
	if req.Model == "pesado" {
		nombre = "right.txt"
	}
	abs := filepath.Join(req.RoomPath, "src", nombre)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return loop.Result{}, err
	}
	if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
		return loop.Result{}, err
	}
	return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
}

// ejecutorConArchivo escribe siempre el mismo archivo en el cuarto: el
// contrato de la subtarea decide si eso basta para quedar verde.
type ejecutorConArchivo struct{ escribe string }

func (e ejecutorConArchivo) Name() string { return "con-archivo" }

func (e ejecutorConArchivo) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	abs := filepath.Join(req.RoomPath, "src", e.escribe)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return loop.Result{}, err
	}
	if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
		return loop.Result{}, err
	}
	return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
}

// agenteDePrueba arma un Agent listo para correr en root, con Root y
// RaizID fijos para que el árbol y los latidos caigan en el repo.
func agenteDePrueba(root string, tarea task.Task, ej loop.Agent, gen plan.Generador) Agent {
	return Agent{
		Cfg: config.Config{
			Modelos: map[string]string{"liviana": "liviano", "media": "pesado", "pesada": "pesado"},
		},
		Constitucion:   "",
		Planificador:   gen,
		Ejecutor:       ej,
		ModeloEjecutor: "liviano",
		Task:           tarea,
		Root:           root,
		RaizID:         tarea.ID,
	}
}

func TestEscalaAModeloPesadoCuandoLaHojaDejaTrabajo(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[{"titulo": "lo de siempre", "porque": "es la pieza", "listo_cuando": "test -f src/right.txt", "tocar_solo": ["src/**"], "peso": "liviana"}]`
	a := agenteDePrueba(root, parentTask, ejecutorPorModelo{}, generadorFalso{texto: texto})

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run debió escalar y quedar verde: %v", err)
	}
	if res.Tokens.Entrada+res.Tokens.Salida < 2 {
		t.Errorf("tokens = %+v, quiero los de los dos intentos (hoja + escalado)", res.Tokens)
	}
	if _, err := os.Stat(filepath.Join(parentRoom.Path, "src", "right.txt")); err != nil {
		t.Fatalf("right.txt no se integró al cuarto padre: %v", err)
	}
	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil || len(nodos) != 1 {
		t.Fatalf("árbol = %+v, %v", nodos, err)
	}
	if !nodos[0].Verde || nodos[0].Modelo != "pesado" {
		t.Errorf("nodo = %+v, quiero verde con modelo pesado", nodos[0])
	}
}

func TestHojaRojaNoMataALasHermanas(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()           // listo_cuando: test -f src/done.txt
	parentTask.ListoCuando = "test -f src/wrong.txt" // pasa cuando sub2 integra
	parentTask.LimiteSubtareas = 2
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	// sub1 siempre roja (escribe wrong, busca right); sub2 verde (escribe
	// wrong, busca wrong); el listo del padre pasa cuando sub2 integra.
	texto := `[
		{"titulo": "falla", "listo_cuando": "test -f src/right.txt", "tocar_solo": ["src/**"], "peso": "liviana"},
		{"titulo": "funciona", "listo_cuando": "test -f src/wrong.txt", "tocar_solo": ["src/**"], "peso": "liviana"}
	]`
	a := agenteDePrueba(root, parentTask, ejecutorConArchivo{escribe: "wrong.txt"}, generadorFalso{texto: texto})

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run debió seguir pese a la hoja roja y quedar verde por el listo del padre: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, quiero 0", res.ExitCode)
	}
	if _, err := os.Stat(filepath.Join(parentRoom.Path, "src", "wrong.txt")); err != nil {
		t.Fatalf("el trabajo de la subtarea verde no se integró: %v", err)
	}
	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil {
		t.Fatalf("árbol: %v", err)
	}
	if len(nodos) != 2 {
		t.Fatalf("árbol = %d nodos, quiero 2", len(nodos))
	}
	for _, n := range nodos {
		if n.ID == parentTask.ID+"001" && n.Verde {
			t.Errorf("sub1 quedó verde, debía quedar roja: %+v", n)
		}
		if n.ID == parentTask.ID+"002" && !n.Verde {
			t.Errorf("sub2 quedó roja, debía quedar verde: %+v", n)
		}
	}
}

func TestHojaRojaSinSalidaDetieneConErrDetener(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentTask.ListoCuando = "test -f src/right.txt"
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[{"titulo": "imposible", "listo_cuando": "test -f src/right.txt", "tocar_solo": ["src/**"], "peso": "liviana"}]`
	a := agenteDePrueba(root, parentTask, ejecutorConArchivo{escribe: "wrong.txt"}, generadorFalso{texto: texto})

	_, err = a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	var detener *loop.ErrDetener
	if !errors.As(err, &detener) {
		t.Fatalf("Agent.Run debió devolver loop.ErrDetener, dio: %v", err)
	}
	if !strings.Contains(detener.Motivo, parentTask.ID+"001") {
		t.Errorf("motivo sin la hoja roja: %s", detener.Motivo)
	}
}

func TestSupervisorReplanificaLaHojaRota(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	descomp := `[{"titulo": "pieza rota", "listo_cuando": "test -f src/right.txt", "tocar_solo": ["src/**"], "peso": "liviana"}]`
	dictamen := `{"decision":"replanificar","motivo":"el alcance apuntaba a otro archivo","contrato":{"titulo":"pieza arreglada","listo_cuando":"test -f src/wrong.txt","tocar_solo":["src/**"],"porque":"se reescribe","como":"escribir wrong.txt"}}`
	gen := &generadorSecuencial{textos: []string{descomp, dictamen}}

	a := agenteDePrueba(root, parentTask, ejecutorConArchivo{escribe: "wrong.txt"}, gen)

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run debió replanificar y quedar verde: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, quiero 0", res.ExitCode)
	}
	if _, err := os.Stat(filepath.Join(parentRoom.Path, "src", "wrong.txt")); err != nil {
		t.Fatalf("el trabajo de la replan no se integró: %v", err)
	}
	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil || len(nodos) != 1 {
		t.Fatalf("árbol = %+v, %v", nodos, err)
	}
	if !nodos[0].Verde {
		t.Errorf("la subtarea replanificada debió quedar verde: %+v", nodos[0])
	}
}

func TestAutoSubagentes(t *testing.T) {
	casos := []struct {
		tareas int
		want   int
	}{
		{0, 1}, {1, 1}, {3, 3}, {8, 8}, {15, 8},
	}
	for _, c := range casos {
		if got := autoSubagentes(c.tareas); got != c.want {
			t.Errorf("autoSubagentes(%d) = %d, quiero %d", c.tareas, got, c.want)
		}
	}
}

func TestModeloEscalado(t *testing.T) {
	cfg := config.Config{Modelos: map[string]string{"liviana": "l", "media": "m", "pesada": "p"}}
	if got := cfg.ModeloEscalado("liviana", "l"); got != "m" {
		t.Errorf("liviana escaló a %q, quiero m", got)
	}
	if got := cfg.ModeloEscalado("media", "m"); got != "" {
		t.Errorf("media escaló a %q, nunca a pesada", got)
	}
	if got := cfg.ModeloEscalado("pesada", "p"); got != "" {
		t.Errorf("pesada escaló a %q, quiero nada", got)
	}
	// el siguiente peso usa el mismo modelo que el hoja: escalar no
	// cambia nada, no se gasta otra invocación
	cfg2 := config.Config{Modelos: map[string]string{"liviana": "l", "media": "l"}}
	if got := cfg2.ModeloEscalado("liviana", "l"); got != "" {
		t.Errorf("escaló a %q pese a ser el mismo modelo", got)
	}
}

func TestTareaDesdeBorradorPropagaContexto(t *testing.T) {
	a := Agent{Task: task.Task{
		ID: "T-100", NoTocar: []string{"z/**"}, Riesgos: "riesgo padre", LimiteLineas: 500,
	}}
	b := plan.Borrador{
		Titulo: "x", ListoCuando: "test -f src/x", TocarSolo: []string{"src/**"},
		NoTocar: []string{"y/**"}, Riesgos: "riesgo hijo", Como: "enfocar así",
	}
	sub, err := a.tareaDesdeBorrador(b, 1)
	if err != nil {
		t.Fatalf("tareaDesdeBorrador: %v", err)
	}
	if sub.Notas != "enfocar así" {
		t.Errorf("Notas = %q, quiero el enfoque del orquestador", sub.Notas)
	}
	if !strings.Contains(strings.Join(sub.NoTocar, ","), "z/**") || !strings.Contains(strings.Join(sub.NoTocar, ","), "y/**") {
		t.Errorf("NoTocar no hereda el del padre: %v", sub.NoTocar)
	}
	if !strings.Contains(sub.Riesgos, "riesgo padre") || !strings.Contains(sub.Riesgos, "riesgo hijo") {
		t.Errorf("Riesgos no hereda contexto: %s", sub.Riesgos)
	}
	if sub.LimiteLineas != 500 {
		t.Errorf("LimiteLineas = %d, quiero el del padre (500)", sub.LimiteLineas)
	}
}

func TestParseDictamen(t *testing.T) {
	d, err := parseDictamen(`{"decision":"omitir","motivo":"no hace falta"}`)
	if err != nil || d.Decision != "omitir" {
		t.Fatalf("parseDictamen = %+v, %v", d, err)
	}
	d, err = parseDictamen("```json\n{\"decision\":\"replanificar\",\"contrato\":{\"titulo\":\"n\",\"listo_cuando\":\"t\"}}\n```")
	if err != nil || d.Decision != "replanificar" || d.Contrato == nil {
		t.Fatalf("parseDictamen con vallas = %+v, %v", d, err)
	}
	if _, err := parseDictamen("texto suelto"); err == nil {
		t.Error("texto no JSON debió fallar")
	}
}

func TestListoPadreVerde(t *testing.T) {
	dir := t.TempDir()
	if listoPadreVerde(context.Background(), dir, "test -f ok.txt", 0) {
		t.Error("ok.txt no existe, debía fallar")
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !listoPadreVerde(context.Background(), dir, "test -f ok.txt", 0) {
		t.Error("ok.txt existe, debía pasar")
	}
}

// numeroDeSub saca el número de la subtarea del nombre de su cuarto:
// ".loom/rooms/T-100001" → "1" (el id de la subtarea es el del padre
// más el índice de 3 dígitos, que son los últimos caracteres).
func numeroDeSub(req loop.Request) string {
	base := filepath.Base(req.RoomPath)
	if i := strings.LastIndex(base, "-"); i >= 0 && len(base)-i >= 4 {
		n := strings.TrimLeft(base[i+1:][len(base[i+1:])-3:], "0")
		if n == "" {
			n = "0"
		}
		return n
	}
	return base
}

// ejecutorPorID escribe src/archivo<N>.txt según la subtarea (N de su
// id), para hojas independientes con listo_cuando propio.
type ejecutorPorID struct{}

func (ejecutorPorID) Name() string { return "por-id" }

func (ejecutorPorID) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	dir, nombre := "uno", "archivo1.txt"
	if numeroDeSub(req) == "2" {
		dir, nombre = "dos", "archivo2.txt"
	}
	abs := filepath.Join(req.RoomPath, "src", dir, nombre)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return loop.Result{}, err
	}
	if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
		return loop.Result{}, err
	}
	return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
}

func TestSubtareasIndependientesCorrenParalelasYSeIntegran(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentTask.LimiteSubtareas = 2
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[
		{"titulo": "uno", "listo_cuando": "test -f src/uno/archivo1.txt", "tocar_solo": ["src/uno/**"], "peso": "liviana"},
		{"titulo": "dos", "listo_cuando": "test -f src/dos/archivo2.txt", "tocar_solo": ["src/dos/**"], "peso": "liviana"}
	]`
	a := agenteDePrueba(root, parentTask, ejecutorPorID{}, generadorFalso{texto: texto})
	a.Cfg.Subagentes = 2

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, quiero 0", res.ExitCode)
	}
	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil || len(nodos) != 2 {
		t.Fatalf("árbol = %+v, %v", nodos, err)
	}
	for _, n := range nodos {
		if !n.Verde {
			t.Errorf("la subtarea %s debió quedar verde: %+v", n.ID, n)
		}
	}
}

// ejecutorConDep solo deja verde a la subtarea 2 si la 1 ya integró su
// archivo al cuarto padre: prueba que la dependencia por índice hace
// esperar a la hoja dependiente.
type ejecutorConDep struct{}

func (ejecutorConDep) Name() string { return "con-dep" }

func (ejecutorConDep) Run(_ context.Context, req loop.Request) (loop.Result, error) {
	base := filepath.Join(req.RoomPath, "src")
	if numeroDeSub(req) == "1" {
		abs := filepath.Join(base, "base", "a.txt")
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return loop.Result{}, err
		}
		if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
			return loop.Result{}, err
		}
		return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
	}
	// la 2 solo escribe si ya ve el a.txt de la 1 en su cuarto (heredado
	// de la rama del padre, que ya integró a la 1)
	if _, err := os.Stat(filepath.Join(base, "base", "a.txt")); err == nil {
		abs := filepath.Join(base, "consumidor", "b.txt")
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return loop.Result{}, err
		}
		if err := os.WriteFile(abs, []byte("ok"), 0o644); err != nil {
			return loop.Result{}, err
		}
	}
	return loop.Result{Tokens: loop.Tokens{Entrada: 1, Salida: 1}}, nil
}

func TestDependenciaPorIndiceHaceEsperarALaHermana(t *testing.T) {
	root := repoConCommit(t)
	parentTask := tareaRecursivaDePrueba()
	parentTask.LimiteSubtareas = 2
	parentRoom, err := room.Create(context.Background(), root, parentTask.ID, "main")
	if err != nil {
		t.Fatalf("room.Create padre: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, parentTask.ID) })

	texto := `[
		{"titulo": "base", "listo_cuando": "test -f src/base/a.txt", "tocar_solo": ["src/base/**"], "peso": "liviana"},
		{"titulo": "consumidor", "listo_cuando": "test -f src/consumidor/b.txt", "tocar_solo": ["src/consumidor/**"], "peso": "liviana", "depende_de": [1]}
	]`
	a := agenteDePrueba(root, parentTask, ejecutorConDep{}, generadorFalso{texto: texto})
	a.Cfg.Subagentes = 2

	res, err := a.Run(context.Background(), loop.Request{RoomPath: parentRoom.Path})
	if err != nil {
		t.Fatalf("Agent.Run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, quiero 0", res.ExitCode)
	}
	if _, err := os.Stat(filepath.Join(parentRoom.Path, "src", "consumidor", "b.txt")); err != nil {
		t.Fatalf("la dependiente no vio el trabajo de la base: %v", err)
	}
	nodos, err := LeerArbol(root, parentTask.ID)
	if err != nil || len(nodos) != 2 {
		t.Fatalf("árbol = %+v, %v", nodos, err)
	}
	for _, n := range nodos {
		if !n.Verde {
			t.Errorf("la subtarea %s debió quedar verde: %+v", n.ID, n)
		}
	}
}

func TestDependenciaSibling(t *testing.T) {
	if got := dependenciaSibling("T-100", "1"); got != "T-100001" {
		t.Errorf("índice 1 = %q, quiero T-100001", got)
	}
	if got := dependenciaSibling("T-100", "T-200"); got != "T-200" {
		t.Errorf("id completo = %q, quiero T-200", got)
	}
}

func TestSubtareaHeredaDisenoSinTopeInventado(t *testing.T) {
	a := Agent{Task: task.Task{ID: "T-001", Notas: "Arquitectura: src/domain define tipos compartidos", LimiteLineas: 0}}
	sub, err := a.tareaDesdeBorrador(plan.Borrador{Titulo: "dominio", ListoCuando: "false", Como: "Validar entradas", LimiteLineas: 200}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if sub.LimiteLineas != 0 || !strings.Contains(sub.Notas, a.Task.Notas) || !strings.Contains(sub.Notas, "Validar entradas") {
		t.Fatalf("contrato: %+v", sub)
	}
}
