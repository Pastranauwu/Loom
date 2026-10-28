package ship

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// cuartoDeTarea crea el cuarto de una tarea con un archivo propio y un
// commit wip, como lo dejaría el bucle.
func cuartoDeTarea(t *testing.T, root, id, archivo string) {
	t.Helper()
	r, err := room.Create(context.Background(), root, id, "main")
	if err != nil {
		t.Fatalf("room.Create %s: %v", id, err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, id) })
	escribir(t, r.Path, archivo, "package a\n\n// "+id+"\n")
	gitCmd(t, r.Path, "add", "-A")
	gitCmd(t, r.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip: "+id+" intento 1")
}

func tareaEntrega(id, archivo string, deps ...string) task.Task {
	return task.Task{
		Version: task.Version, ID: id, Titulo: "tarea " + id,
		ListoCuando: "true", TocarSolo: []string{archivo},
		DependeDe:      deps,
		LimiteIntentos: 3, LimiteLineas: 200,
	}
}

// sinIdentidadGit deja el repo como un runner de CI limpio: sin user.name
// ni user.email. Cualquier commit que loom cree ahí tiene que traer su
// propia identidad, o git responde "empty ident name" y la entrega muere.
func sinIdentidadGit(t *testing.T, root string) {
	t.Helper()
	gitCmd(t, root, "config", "user.name", "")
	gitCmd(t, root, "config", "user.email", "")
}

// La entrega conjunta deja UN commit por tarea sobre la base, en orden de
// dependencia: es lo que separa un PR limpio de N PRs que se pisan.
func TestEntregarTodasUnCommitPorTarea(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")
	cuartoDeTarea(t, root, "T-002", "b.go")

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{
			tareaEntrega("T-002", "b.go", "T-001"),
			tareaEntrega("T-001", "a.go"),
		},
		DryRun: true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	if !e.Aprobado {
		t.Fatalf("entrega no aprobada · %s · pasos %+v", e.PrimerMotivo(), e.Pasos)
	}
	if len(e.Tareas) != 2 {
		t.Fatalf("tareas = %d, quiero 2", len(e.Tareas))
	}

	log := gitCmd(t, root, "log", "--format=%s", "main.."+RamaEntrega)
	lineas := strings.Fields(strings.ReplaceAll(strings.TrimSpace(log), "\n", " "))
	if len(lineas) == 0 {
		t.Fatal("la rama de entrega no tiene commits")
	}
	commits := strings.Split(strings.TrimSpace(log), "\n")
	if len(commits) != 2 {
		t.Errorf("commits = %d (%q), quiero uno por tarea", len(commits), log)
	}
	// git log va del más nuevo al más viejo: T-001 debe quedar debajo
	if !strings.Contains(commits[len(commits)-1], "T-001") {
		t.Errorf("T-001 debe ser el primer commit · %q", log)
	}
	// nada de wip en el historial entregado
	if strings.Contains(log, "wip:") {
		t.Errorf("el PR no debe llevar commits wip · %q", log)
	}
}

// Dos tareas verdes por separado que se rompen juntas: la esclusa de
// cada una pasa, y solo la corrida sobre el conjunto integrado lo ve.
// Sin ese paso, el PR saldría roto.
func TestEntregarTodasFrenaSiElConjuntoFalla(t *testing.T) {
	root := repoConCommit(t)
	cuartoDeTarea(t, root, "T-001", "a.go")
	cuartoDeTarea(t, root, "T-002", "b.go")

	// verdadero mientras falte alguno de los dos archivos: cada cuarto
	// tiene solo el suyo, la rama de entrega tiene los dos
	pruebas := `[ ! -f a.go ] || [ ! -f b.go ]`

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: pruebas},
		Base:   "main",
		Tareas: []task.Task{
			tareaEntrega("T-001", "a.go"),
			tareaEntrega("T-002", "b.go"),
		},
		DryRun: true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	// las dos esclusas individuales tienen que haber pasado
	for _, r := range e.Tareas {
		if !r.Aprobado {
			t.Fatalf("%s debía pasar sola · %s", r.ID, r.PrimerMotivo())
		}
	}
	if e.Aprobado {
		t.Error("con las pruebas del conjunto en rojo no debe aprobarse")
	}
	if !strings.Contains(e.PrimerMotivo(), "el conjunto falla") {
		t.Errorf("motivo = %q", e.PrimerMotivo())
	}
}

// Escenario real de oleadas: T-002 nace desde la rama de integración, que
// ya trae el trabajo de T-001. Su commit debe llevar SOLO lo suyo; si
// arrastra los archivos de T-001, el cherry-pick choca y no hay PR.
const lineasDeB = 3

func TestEntregarTodasSegundaOleadaNoArrastraLaPrimera(t *testing.T) {
	root := repoConCommit(t)
	cuartoDeTarea(t, root, "T-001", "a.go")

	// la integración de la oleada 1, como la arma internal/room
	if err := room.ResetIntegration(context.Background(), root, "main"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, "_integra") })
	if salida, err := room.Integrar(context.Background(), root, "T-001"); err != nil {
		t.Fatalf("integrar: %v %s", err, salida)
	}

	// T-002 nace desde la integración: su rama ya contiene a.go
	r2, err := room.Create(context.Background(), root, "T-002", room.IntegrationBranch)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, "T-002") })
	if _, err := os.Stat(filepath.Join(r2.Path, "a.go")); err != nil {
		t.Fatalf("el cuarto de la oleada 2 debería traer a.go · %v", err)
	}
	escribir(t, r2.Path, "b.go", "package a\n\n// T-002\n") // 3 líneas, contra las 10 de a.go
	gitCmd(t, r2.Path, "add", "-A")
	gitCmd(t, r2.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip: T-002 intento 1")

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{
			tareaEntrega("T-001", "a.go"),
			tareaEntrega("T-002", "b.go", "T-001"),
		},
		DryRun: true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	if !e.Aprobado {
		t.Fatalf("entrega no aprobada · %s · pasos %+v", e.PrimerMotivo(), e.Pasos)
	}

	// la esclusa de T-002 debe medir SOLO su propio trabajo. Aplanada
	// contra la rama base en vez de contra su punto de partida, contaría
	// también las líneas de a.go que heredó de la oleada anterior.
	for _, r := range e.Tareas {
		if r.ID != "T-002" {
			continue
		}
		if r.LineasMas != lineasDeB {
			t.Errorf("T-002 reporta +%d líneas, quiero +%d · está arrastrando el trabajo de T-001",
				r.LineasMas, lineasDeB)
		}
	}

	commits := strings.Split(strings.TrimSpace(gitCmd(t, root, "log", "--format=%H", "main.."+RamaEntrega)), "\n")
	if len(commits) != 2 {
		t.Fatalf("commits = %d, quiero 2", len(commits))
	}
	// git log va del más nuevo al más viejo: commits[0] es T-002
	tocados := strings.Fields(gitCmd(t, root, "show", "--format=", "--name-only", commits[0]))
	if len(tocados) != 1 || tocados[0] != "b.go" {
		t.Errorf("el commit de T-002 toca %v, quiero solo [b.go]", tocados)
	}
	// y la rama entregada tiene los dos archivos
	for _, f := range []string{"a.go", "b.go"} {
		if out := gitCmd(t, root, "ls-tree", "--name-only", RamaEntrega, f); strings.TrimSpace(out) != f {
			t.Errorf("falta %s en la rama de entrega", f)
		}
	}
}

// Dos tareas que escriben el mismo archivo con contenido distinto: eso sí
// es un solapamiento de alcances, y el mensaje debe decirlo con el
// archivo en la mano.
func TestEntregarTodasReportaElConflictoReal(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)

	for _, c := range []struct{ id, cuerpo string }{
		{"T-001", "package a\n\n// version de T-001\n"},
		{"T-002", "package a\n\n// version distinta de T-002\n"},
	} {
		r, err := room.Create(context.Background(), root, c.id, "main")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = room.Destroy(context.Background(), root, c.id) })
		escribir(t, r.Path, "choque.go", c.cuerpo)
		gitCmd(t, r.Path, "add", "-A")
		gitCmd(t, r.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip: "+c.id+" intento 1")
	}

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{
			tareaEntrega("T-001", "choque.go"),
			tareaEntrega("T-002", "choque.go"),
		},
		DryRun: true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	if e.Aprobado {
		t.Fatal("dos tareas sobre el mismo archivo no pueden integrarse solas")
	}
	motivo := e.PrimerMotivo()
	if !strings.Contains(motivo, "choque.go") || !strings.Contains(motivo, "tocar_solo") {
		t.Errorf("motivo = %q · debe nombrar el archivo y el solapamiento", motivo)
	}
}

// La esclusa tiene que dar el mismo presupuesto la segunda vez. Tras la
// primera pasada la rama queda aplanada y ya no hay commits `wip:` que
// mirar: sin el commit de arranque anotado en el estado, la segunda medía
// el cuarto entero contra la rama base y frenaba por un presupuesto
// inflado que nadie habia excedido.
func TestEntregarTodasEsIdempotente(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")

	arranque := strings.TrimSpace(gitCmd(t, root, "rev-parse", "main"))
	opciones := func(commits map[string]string) OpcionesEntrega {
		return OpcionesEntrega{
			Root:    root,
			Config:  config.Config{Base: "main", Pruebas: "true"},
			Base:    "main",
			Tareas:  []task.Task{tareaEntrega("T-001", "a.go")},
			Commits: commits,
			DryRun:  true,
		}
	}
	conCommit := map[string]string{"T-001": arranque}

	correr := func(commits map[string]string) Entrega {
		e := EntregarTodas(context.Background(), opciones(commits))
		_ = limpiarEntrega(root, roomPathDe(root, "_entrega"))
		return e
	}

	primera := correr(conCommit)
	if !primera.Aprobado {
		t.Fatalf("primera pasada · %s", primera.PrimerMotivo())
	}
	// la rama del cuarto vuelve a su punta: lo aplanado vive en la rama
	// de entrega, y las tareas que dependen de esta siguen encima de sus
	// commits originales
	if log := gitCmd(t, root, "log", "--format=%s", "main..loom/T-001"); !strings.Contains(log, "wip:") {
		t.Fatalf("la entrega no debe dejar reescrita la rama del cuarto · %q", log)
	}

	segunda := correr(conCommit)
	if !segunda.Aprobado {
		t.Fatalf("segunda pasada · %s", segunda.PrimerMotivo())
	}
	if primera.Tareas[0].LineasMas != segunda.Tareas[0].LineasMas {
		t.Errorf("lineas: primera %d, segunda %d · la esclusa debe medir lo mismo",
			primera.Tareas[0].LineasMas, segunda.Tareas[0].LineasMas)
	}
}

// Un cuarto de antes de que se anotara el commit de arranque: su rama ya
// viene aplanada por una pasada previa y no tiene marcadores `wip:`. La
// esclusa tiene que reconocer esa forma en vez de medir contra la base.
func TestEntregarTodasReconoceRamaYaAplanada(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")
	arranque := strings.TrimSpace(gitCmd(t, root, "rev-parse", "main"))

	base := OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{tareaEntrega("T-001", "a.go")},
		DryRun: true,
	}

	conAnotacion := base
	conAnotacion.Commits = map[string]string{"T-001": arranque}
	primera := EntregarTodas(context.Background(), conAnotacion)
	_ = limpiarEntrega(root, roomPathDe(root, "_entrega"))
	if !primera.Aprobado {
		t.Fatalf("primera pasada · %s", primera.PrimerMotivo())
	}

	// segunda pasada SIN anotación, como un cuarto viejo
	segunda := EntregarTodas(context.Background(), base)
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })
	if !segunda.Aprobado {
		t.Fatalf("segunda pasada · %s", segunda.PrimerMotivo())
	}
	if primera.Tareas[0].LineasMas != segunda.Tareas[0].LineasMas {
		t.Errorf("lineas: con anotación %d, sin ella %d · debe reconocer la rama aplanada",
			primera.Tareas[0].LineasMas, segunda.Tareas[0].LineasMas)
	}
}

// revisorFijo responde siempre lo mismo, para probar las dos ramas del
// paso de integración sin hablar con un modelo.
type revisorFijo struct {
	sinCambios bool
	informe    string
	resumen    string
	err        error
}

func (r revisorFijo) Revisar(context.Context, string, []task.Task) (bool, string, string, error) {
	return r.sinCambios, r.informe, r.resumen, r.err
}

// El revisor informa, no decide: cuando pide cambios, el PR se queda
// abierto con el informe dentro y el merge no se intenta. Se prueba sobre
// revisarEntrega porque en --dry-run la entrega ni llega ahí.
func TestRevisarEntregaReportaCambios(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)

	var pasos []Paso
	sinCambios, ok := revisarEntrega(context.Background(),
		OpcionesEntrega{Root: root, Base: "main",
			Revisor: revisorFijo{sinCambios: false, informe: "## Revisión", resumen: "1 de 1 tareas necesitan cambios: T-001"}},
		root, "main", "http://pr/1", nil, func(p Paso) { pasos = append(pasos, p) })

	if sinCambios {
		t.Error("el revisor pidió cambios · no puede reportar lo contrario")
	}
	_ = ok // sin remoto, publicar el comentario falla; lo que importa es el veredicto
	if len(pasos) != 1 {
		t.Fatalf("pasos = %+v", pasos)
	}
}

// Lo que no se pudo revisar no se da por revisado: falla cerrado, y el
// paso dice que nadie miró el diff.
func TestRevisarEntregaFallaCerrado(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)

	var pasos []Paso
	sinCambios, ok := revisarEntrega(context.Background(),
		OpcionesEntrega{Root: root, Base: "main", Revisor: revisorFijo{err: errors.New("el revisor no respondió")}},
		root, "main", "http://pr/1", nil, func(p Paso) { pasos = append(pasos, p) })

	if sinCambios || ok {
		t.Error("una revisión fallida no puede darse por buena")
	}
	if len(pasos) != 1 || pasos[0].OK {
		t.Fatalf("pasos = %+v", pasos)
	}
	if !strings.Contains(pasos[0].Detalle, "sin revisar") {
		t.Errorf("detalle = %q · debe decir que nadie miró el diff", pasos[0].Detalle)
	}
}

// --dry-run e --integrar se contradicen: la entrega en seco termina antes
// de tocar la integración, sin llamar siquiera al revisor.
func TestDryRunNoIntegraAunqueSePida(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:     root,
		Config:   config.Config{Base: "main", Pruebas: "true"},
		Base:     "main",
		Tareas:   []task.Task{tareaEntrega("T-001", "a.go")},
		Commits:  map[string]string{"T-001": strings.TrimSpace(gitCmd(t, root, "rev-parse", "main"))},
		Revisor:  revisorFijo{err: errors.New("no debería llamarse")},
		Integrar: true,
		DryRun:   true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	if !e.Aprobado {
		t.Fatalf("la entrega en seco debe aprobarse · %s", e.PrimerMotivo())
	}
	if e.Integrado {
		t.Error("--dry-run no integra nada")
	}
}

// un esqueleto que solo planeó cambios a código existente quedó
// verde sin cambios propios y frenaba la entrega con "nada que entregar"
func TestEntregarTodasOmiteLaTareaSinCambios(t *testing.T) {
	root := repoConCommit(t)
	cuartoDeTarea(t, root, "T-002", "b.go")
	if _, err := room.Create(context.Background(), root, "T-001", "main"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, "T-001") })

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{tareaEntrega("T-001", "a.go"), tareaEntrega("T-002", "b.go", "T-001")},
		DryRun: true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })
	if !e.Aprobado {
		t.Fatalf("entrega frenada · %s", e.PrimerMotivo())
	}
	if log := gitCmd(t, root, "log", "--format=%s", "main.."+RamaEntrega); strings.Count(strings.TrimSpace(log), "\n") != 0 {
		t.Errorf("quiero solo el commit de T-002 · %q", log)
	}
}

func TestAceptacionDeDistinguePasoFalloYNoCorrio(t *testing.T) {
	o := OpcionesEntrega{
		Tareas:     []task.Task{{ID: "T-001"}, {ID: "T-002"}},
		Acceptance: []string{"go build ./...", "go test ./...", "make e2e"},
	}
	e := Entrega{Pasos: []Paso{
		{"integradas", true, "go test ./..."},
		{"aceptación", true, "go build ./..."},
		{"aceptación", false, "go test ./... · FAIL"},
	}}
	a := aceptacionDe(o, e)
	quiero := []Criterio{{"go build ./...", true, true}, {"go test ./...", true, false}, {"make e2e", false, false}}
	if len(a.Criterios) != 3 || a.Criterios[0] != quiero[0] || a.Criterios[1] != quiero[1] || a.Criterios[2] != quiero[2] {
		t.Errorf("criterios = %+v", a.Criterios)
	}
	if a.Aprobado || len(a.Tareas) != 2 || !strings.Contains(a.Motivo, "FAIL") {
		t.Errorf("aceptación = %+v", a)
	}

	root := t.TempDir()
	if err := guardarAceptacion(root, a); err != nil {
		t.Fatal(err)
	}
	if leida, ok := LeerAceptacion(root); !ok || len(leida.Criterios) != 3 || leida.Motivo != a.Motivo {
		t.Errorf("leída = %+v ok = %v", leida, ok)
	}
	if _, ok := LeerAceptacion(t.TempDir()); ok {
		t.Error("sin archivo no hay aceptación")
	}
}

// cada runner nombra sus fallos a su modo: de ahí salen el archivo (para
// saber qué tarea reabrir) y el selector (para correr solo lo que falló)
func TestFalloDeReconoceUnittestYPytest(t *testing.T) {
	dir := t.TempDir()
	escribir(t, dir, "apps/docs/tests/test_flujo.py", "")
	crudo := "======\nFAIL: test_pantalla (apps.docs.tests.test_flujo.PantallaTest.test_pantalla)\n------\nAssertionError: 404 != 200\n"
	f := falloDe(dir, "integradas", "python manage.py test", "AssertionError: 404 != 200", crudo)
	if len(f.Pruebas) != 1 || f.Pruebas[0] != "apps/docs/tests/test_flujo.py" {
		t.Errorf("pruebas = %v", f.Pruebas)
	}
	if len(f.Selectores) != 1 || f.Selectores[0] != "apps.docs.tests.test_flujo.PantallaTest.test_pantalla" {
		t.Errorf("selectores = %v", f.Selectores)
	}
	if len(f.Nombres) != 1 || !strings.HasPrefix(f.Nombres[0], "test_pantalla (") {
		t.Errorf("nombres = %v", f.Nombres)
	}
	// unittest de antes de 3.11: el paréntesis no trae el método
	f = falloDe(dir, "integradas", "python -m unittest", "", "ERROR: test_pantalla (apps.docs.tests.test_flujo.PantallaTest)\n")
	if len(f.Selectores) != 1 || f.Selectores[0] != "apps.docs.tests.test_flujo.PantallaTest.test_pantalla" {
		t.Errorf("selectores 3.10 = %v", f.Selectores)
	}

	f = falloDe(dir, "integradas", "pytest", "", "FAILED tests/test_api.py::test_alta - assert 404 == 200\n")
	if len(f.Pruebas) != 1 || f.Pruebas[0] != "tests/test_api.py" || f.Selectores[0] != "tests/test_api.py::test_alta" {
		t.Errorf("pytest: pruebas = %v selectores = %v", f.Pruebas, f.Selectores)
	}
	// go test da solo el nombre: queda dicho, sin archivo ni selector
	f = falloDe(dir, "integradas", "go test ./...", "", "--- FAIL: TestSuma (0.00s)\n")
	if len(f.Nombres) != 1 || f.Nombres[0] != "TestSuma" || len(f.Pruebas)+len(f.Selectores) != 0 {
		t.Errorf("go: %+v", f)
	}
}

func TestFalloDeSacaLasPruebasQueFallaron(t *testing.T) {
	f := falloDe(t.TempDir(), "integradas", "npx vitest run", "FAIL  tests/screens/tutoriales.test.ts > muestra intro · AssertionError: x · FAIL  tests/screens/tutoriales.test.ts > otra · FAIL  tests/ui/a.test.ts > b", "")
	if len(f.Pruebas) != 2 || f.Pruebas[0] != "tests/screens/tutoriales.test.ts" || f.Pruebas[1] != "tests/ui/a.test.ts" {
		t.Errorf("pruebas = %v", f.Pruebas)
	}
}

// vitest sin archivos cierra con su lista de exclusiones: la entrega tiene
// que decir que la prueba no existe, no "exclude: **/node_modules/**".
func TestEntregarTodasNombraLaPruebaDeAceptacionQueNoExiste(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root: root, Config: config.Config{Base: "main", Pruebas: "true"}, Base: "main",
		Tareas:     []task.Task{tareaEntrega("T-001", "a.go")},
		Acceptance: []string{"false tests/integracion-escucha.test.ts"},
		DryRun:     true,
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })
	if e.Aprobado || e.Fallo == nil || !strings.Contains(e.PrimerMotivo(), "la prueba tests/integracion-escucha.test.ts no existe") {
		t.Fatalf("motivo = %q fallo = %+v", e.PrimerMotivo(), e.Fallo)
	}
	if a, ok := LeerAceptacion(root); !ok || a.Fallo == nil || a.Fallo.Paso != "aceptación" {
		t.Errorf("el fallo debe quedar en aceptacion.json para loom reparar: %+v", a)
	}
}

// La entrega aplana las ramas para integrarlas, pero al salir las deja
// como estaban: las tareas que dependen de ellas se construyeron sobre
// sus commits originales y `run` tiene que poder volver a juntarlas.
func TestEntregarTodasDejaLasRamasDeLosCuartosComoEstaban(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	cuartoDeTarea(t, root, "T-001", "a.go")
	cuartoDeTarea(t, root, "T-002", "b.go")
	antes := map[string]string{}
	for _, id := range []string{"T-001", "T-002"} {
		antes[id] = strings.TrimSpace(gitCmd(t, root, "rev-parse", room.Branch(id)))
	}
	for _, aceptacion := range []string{"true", "false"} { // entrega aprobada y frenada
		e := EntregarTodas(context.Background(), OpcionesEntrega{
			Root: root, Config: config.Config{Base: "main", Pruebas: "true"}, Base: "main",
			Tareas:     []task.Task{tareaEntrega("T-001", "a.go"), tareaEntrega("T-002", "b.go", "T-001")},
			Acceptance: []string{aceptacion},
			DryRun:     true,
		})
		_ = limpiarEntrega(root, roomPathDe(root, "_entrega"))
		if e.Aprobado != (aceptacion == "true") {
			t.Fatalf("aceptación %s: aprobado = %v · %s", aceptacion, e.Aprobado, e.PrimerMotivo())
		}
		for id, punta := range antes {
			if ahora := strings.TrimSpace(gitCmd(t, root, "rev-parse", room.Branch(id))); ahora != punta {
				t.Errorf("aceptación %s: la rama de %s pasó de %s a %s", aceptacion, id, punta, ahora)
			}
		}
	}
}

// con origin pero gh en otra cuenta, una entrega que pasó todas las
// compuertas no se frena en el último paso
func TestEntregarTodasQuedaLocalSiElRemotoNoAbreElPR(t *testing.T) {
	root := repoConCommit(t)
	cuartoDeTarea(t, root, "T-001", "a.go")
	gitCmd(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "no-existe.git"))

	e := EntregarTodas(context.Background(), OpcionesEntrega{
		Root:   root,
		Config: config.Config{Base: "main", Pruebas: "true"},
		Base:   "main",
		Tareas: []task.Task{tareaEntrega("T-001", "a.go")},
	})
	t.Cleanup(func() { _ = limpiarEntrega(root, roomPathDe(root, "_entrega")) })

	if !e.Aprobado || !strings.HasPrefix(e.PR, PRLocal) {
		t.Fatalf("quiero PR local aprobado · PR=%q · %s · pasos %+v", e.PR, e.PrimerMotivo(), e.Pasos)
	}
	if _, err := os.Stat(archivoPRLocal(root, RamaEntrega)); err != nil {
		t.Fatalf("sin descripción del PR local: %v", err)
	}
	if _, err := gitRun(root, "rev-parse", "--verify", "--quiet", RamaEntrega); err != nil {
		t.Fatal("la rama de entrega no quedó para revisarla")
	}
}

// docker solo entra en un release: fuera de él, la aceptación de imágenes
// que guardó un spec viejo se omite y docker ni se llama.
func TestDockerSoloConRelease(t *testing.T) {
	root := repoConCommit(t)
	sinIdentidadGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "compose.yaml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, root, "add", "compose.yaml")
	gitCmd(t, root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "compose")
	cuartoDeTarea(t, root, "T-001", "a.go")

	bin := t.TempDir()
	registro := filepath.Join(t.TempDir(), "docker.log")
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\necho \"$@\" >> "+registro+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, release := range []bool{false, true} {
		os.Remove(registro)
		e := EntregarTodas(context.Background(), OpcionesEntrega{
			Root: root, Config: config.Config{Base: "main", Pruebas: "true"}, Base: "main",
			Tareas:     []task.Task{tareaEntrega("T-001", "a.go")},
			Acceptance: []string{"docker compose build"},
			DryRun:     true,
			Release:    release,
		})
		_ = limpiarEntrega(root, roomPathDe(root, "_entrega"))
		if !e.Aprobado {
			t.Fatalf("release=%v: %s", release, e.PrimerMotivo())
		}
		paso := ""
		for _, p := range e.Pasos {
			if p.Nombre == "docker" {
				paso = p.Detalle
			}
		}
		_, llamado := os.Stat(registro)
		switch {
		case !release && (paso != "" || llamado == nil):
			t.Errorf("sin release docker no corre · paso %q · llamado %v", paso, llamado == nil)
		case release && (paso != "docker compose build" || llamado != nil):
			t.Errorf("con release se construyen las imágenes · paso %q · llamado %v", paso, llamado == nil)
		}
	}
}
