package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/task"
)

func tareaValida() task.Task {
	return task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         "x",
		ListoCuando:    "false",
		TocarSolo:      []string{"src/export/**"},
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}
}

// chequeo busca un chequeo por nombre: el orden de la lista cambia
// cada vez que se agrega uno.
func chequeo(t *testing.T, res Result, nombre string) Check {
	t.Helper()
	for _, c := range res.Chequeos {
		if c.Nombre == nombre {
			return c
		}
	}
	t.Fatalf("no hubo chequeo %q en %+v", nombre, res.Chequeos)
	return Check{}
}

func TestCruceExportado(t *testing.T) {
	otra := task.Task{ID: "T-002", TocarSolo: []string{"src/**"}}
	if c := Cruce(tareaValida(), []task.Task{otra}); c.OK {
		t.Error("Cruce no detectó el solapamiento")
	}
	lejana := task.Task{ID: "T-003", TocarSolo: []string{"docs/**"}}
	if c := Cruce(tareaValida(), []task.Task{lejana}); !c.OK {
		t.Errorf("Cruce rechazó sin solapamiento: %s", c.Motivo)
	}
}

func TestGateTodoVerde(t *testing.T) {
	res := Run(context.Background(), t.TempDir(), config.Config{}, tareaValida(), nil, DefaultTimeout)
	if !res.Aprobada {
		t.Fatalf("tarea válida rechazada: %+v", res.Chequeos)
	}
	if len(res.Chequeos) != 7 {
		t.Fatalf("Chequeos = %d, quiero 7", len(res.Chequeos))
	}
}

func TestGateSinListoCuando(t *testing.T) {
	tarea := tareaValida()
	tarea.ListoCuando = ""
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea sin listo_cuando aprobada")
	}
	if c := chequeo(t, res, "falla hoy"); c.OK {
		t.Errorf("falla hoy debió quedar no evaluado: %+v", c)
	}
}

func TestGateComandoInexistente(t *testing.T) {
	tarea := tareaValida()
	tarea.ListoCuando = "binario-que-no-existe-xyz --flag"
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea con comando inexistente aprobada")
	}
	if got := res.PrimerMotivo(); got == "" {
		t.Error("PrimerMotivo vacío")
	}
}

func TestGateScriptRelativo(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "test.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	tarea := tareaValida()
	tarea.ListoCuando = "./test.sh"
	res := Run(context.Background(), root, config.Config{}, tarea, nil, DefaultTimeout)
	if !res.Aprobada {
		t.Fatalf("script relativo ejecutable rechazado: %+v", res.Chequeos)
	}

	tarea.ListoCuando = "./no-existe.sh"
	res = Run(context.Background(), root, config.Config{}, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("script inexistente aprobado")
	}
}

func TestGateYaPasa(t *testing.T) {
	tarea := tareaValida()
	tarea.ListoCuando = "true"
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea cuyo listo_cuando ya pasa fue aprobada")
	}
	fallaHoy := chequeo(t, res, "falla hoy")
	if fallaHoy.OK || fallaHoy.Motivo == "" {
		t.Errorf("falla hoy debió rechazar: %+v", fallaHoy)
	}
}

func TestGateTimeout(t *testing.T) {
	tarea := tareaValida()
	tarea.ListoCuando = "sleep 5"
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, 100*time.Millisecond)
	if res.Aprobada {
		t.Fatal("tarea con timeout aprobada")
	}
	fallaHoy := chequeo(t, res, "falla hoy")
	if fallaHoy.OK {
		t.Error("timeout debió rechazar la tarea")
	}
}

func TestGateCruce(t *testing.T) {
	otra := task.Task{ID: "T-002", TocarSolo: []string{"src/**"}}
	res := Run(context.Background(), t.TempDir(), config.Config{}, tareaValida(), []task.Task{otra}, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tareas cruzadas aprobadas")
	}

	lejana := task.Task{ID: "T-003", TocarSolo: []string{"docs/**"}}
	res = Run(context.Background(), t.TempDir(), config.Config{}, tareaValida(), []task.Task{lejana}, DefaultTimeout)
	if !res.Aprobada {
		t.Fatalf("tareas sin cruce rechazadas: %+v", res.Chequeos)
	}
}

func TestGateZonasProhibidas(t *testing.T) {
	cfg := config.Config{ZonasProhibidas: config.DefaultForbiddenZones()}

	tarea := tareaValida()
	tarea.TocarSolo = []string{"package-lock.json"}
	res := Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea tocando lockfile aprobada")
	}

	tarea.TocarSolo = []string{"migrations/v2/**"}
	res = Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea tocando migrations aprobada")
	}

	tarea.TocarSolo = []string{"src/export/**"}
	res = Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout)
	if !res.Aprobada {
		t.Fatalf("tarea fuera de zonas prohibidas rechazada: %+v", res.Chequeos)
	}
}

func TestGlobsOverlap(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"src/**", "src/**", true},
		{"src/**", "src/export/**", true},
		{"src/export/**", "src/**", true},
		{"src/auth/**", "src/export/**", false},
		{"package-lock.json", "package-lock.json", true},
		{"CHANGELOG*", "CHANGELOG.md", true},
		{"migrations/**", "migrations/v2/001.sql", true},
		{"src/*", "src/x", true},
		{"docs/**", "src/**", false},
		{"a.go", "b.go", false},
		{"**/*.tsx", "package-lock.json", false},
		{"**/*.json", "package-lock.json", true},
		{"**/*.ts", "migrations/**", true},
		{"**/*.ts", "CHANGELOG*", true},
		{"internal/**/x.go", "internal/y.go", false},
	}
	for _, tc := range cases {
		if got := globsOverlap(tc.a, tc.b); got != tc.want {
			t.Errorf("globsOverlap(%q, %q) = %v, quiero %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAlcanceProhibido(t *testing.T) {
	zonas := []string{"go.sum", "migrations/**", ".github/**"}
	patrones := []string{"*_test.go", "test/**"}
	cases := []struct {
		p        string
		wantZona string
		wantMal  bool
	}{
		{"go.sum", "go.sum", true},
		{"go.mod", "", false}, // no se cruza con go.sum
		{"migrations/001.sql", "migrations/**", true},
		{"internal/wol/**", "", false},
		{"internal/wol/wol_test.go", "*_test.go", true},
		{"test/fixtures/**", "test/**", true},
	}
	for _, tc := range cases {
		z, mal := AlcanceProhibido(tc.p, zonas, patrones)
		if mal != tc.wantMal || z != tc.wantZona {
			t.Errorf("AlcanceProhibido(%q) = (%q, %v), quiero (%q, %v)", tc.p, z, mal, tc.wantZona, tc.wantMal)
		}
	}
}

func TestGateRechazaRutasDePrueba(t *testing.T) {
	for _, ruta := range []string{
		"src/export/*_test.go",
		"test/**",
		"tests/export/**",
		"spec/export.spec.ts",
		"src/export/writer.spec.ts",
	} {
		tarea := tareaValida()
		tarea.TocarSolo = []string{ruta}
		res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
		if res.Aprobada {
			t.Errorf("tocar_solo con %s fue aprobado", ruta)
			continue
		}
		if !strings.Contains(res.PrimerMotivo(), "rutas de prueba") {
			t.Errorf("%s rechazado por otro motivo: %s", ruta, res.PrimerMotivo())
		}
	}
}

func TestGateAceptaAlcanceQueContienePruebas(t *testing.T) {
	// src/export/** contiene archivos _test.go pero no los declara:
	// rechazarlo rechazaría todo contrato razonable
	tarea := tareaValida()
	tarea.TocarSolo = []string{"src/export/**", "internal/kv/kv.go"}
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if !res.Aprobada {
		t.Errorf("alcance normal rechazado: %s", res.PrimerMotivo())
	}
}

func TestGatePatronesDePruebaDeConfig(t *testing.T) {
	cfg := config.Config{PatronesPrueba: []string{"pruebas/**"}}
	tarea := tareaValida()
	tarea.TocarSolo = []string{"pruebas/export/casos.go"}
	if res := Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout); res.Aprobada {
		t.Error("los patrones de config no se aplicaron")
	}
	// con patrones propios, los de por defecto ya no rigen
	tarea.TocarSolo = []string{"spec/export.spec.ts"}
	if res := Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout); !res.Aprobada {
		t.Errorf("los patrones de config debieron sustituir a los de por defecto: %s", res.PrimerMotivo())
	}
}

func TestGateTocarSoloVacioTareaUnica(t *testing.T) {
	tarea := tareaValida()
	tarea.TocarSolo = nil
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if !res.Aprobada {
		t.Errorf("tocar_solo vacío rechazado con una sola tarea: %s", res.PrimerMotivo())
	}
}

func TestGateTocarSoloVacioConOtraActiva(t *testing.T) {
	tarea := tareaValida()
	tarea.TocarSolo = nil
	otra := tareaValida()
	otra.ID = "T-002"
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, []task.Task{otra}, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tocar_solo vacío aprobado con otra tarea en curso")
	}
	quiere := "tocar_solo obligatorio con más de una tarea activa · declara tus rutas"
	if res.PrimerMotivo() != quiere {
		t.Errorf("motivo = %q, quiere %q", res.PrimerMotivo(), quiere)
	}
}

func TestGateRechazaContratoSinVersion(t *testing.T) {
	tarea := tareaValida()
	tarea.Version = 0
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("contrato sin version aprobado")
	}
	if !strings.Contains(res.PrimerMotivo(), "falta version") {
		t.Errorf("motivo = %q · quiere señalar la version que falta", res.PrimerMotivo())
	}
}

func TestGatePropagaElAvisoDeVersionFutura(t *testing.T) {
	tarea := tareaValida()
	tarea.Version = 2
	tarea.Aviso = "contrato versión 2, binario soporta 1 · actualiza loom"
	res := Run(context.Background(), t.TempDir(), config.Config{}, tarea, nil, DefaultTimeout)
	if !res.Aprobada {
		t.Fatalf("un contrato de versión futura válido fue rechazado: %s", res.PrimerMotivo())
	}
	if res.Aviso != tarea.Aviso {
		t.Errorf("Aviso = %q · la esclusa debe propagarlo", res.Aviso)
	}
}

func TestGateAgenteValidoEInvalido(t *testing.T) {
	cfg := config.Config{
		Agentes: map[string]config.Agente{
			"architect": {Provider: "claude", Modelo: "claude-sonnet"},
		},
		Proveedores: map[string]config.Proveedor{
			"planificador": {Modelo: "claude-sonnet"},
		},
	}

	// 1. Sin agente en la tarea -> pasa
	tarea := tareaValida()
	if res := Run(context.Background(), t.TempDir(), cfg, tarea, nil, DefaultTimeout); !res.Aprobada {
		t.Errorf("tarea sin agente rechazada: %s", res.PrimerMotivo())
	}

	// 2. Con agente existente en Agentes -> pasa
	tareaConAgente := tareaValida()
	tareaConAgente.Agente = "architect"
	if res := Run(context.Background(), t.TempDir(), cfg, tareaConAgente, nil, DefaultTimeout); !res.Aprobada {
		t.Errorf("tarea con agente existente rechazada: %s", res.PrimerMotivo())
	}

	// 3. Con agente existente en Proveedores -> pasa
	tareaConProveedor := tareaValida()
	tareaConProveedor.Agente = "planificador"
	if res := Run(context.Background(), t.TempDir(), cfg, tareaConProveedor, nil, DefaultTimeout); !res.Aprobada {
		t.Errorf("tarea con agente en proveedores rechazada: %s", res.PrimerMotivo())
	}

	// 4. Con arquetipo por defecto (backend, frontend) sin configurar en config.yml -> pasa
	tareaBackend := tareaValida()
	tareaBackend.Agente = "backend"
	if res := Run(context.Background(), t.TempDir(), config.Config{}, tareaBackend, nil, DefaultTimeout); !res.Aprobada {
		t.Errorf("tarea con arquetipo backend rechazada: %s", res.PrimerMotivo())
	}

	// 5. Con agente inexistente -> rechazada
	tareaDesconocido := tareaValida()
	tareaDesconocido.Agente = "no-existe"
	res := Run(context.Background(), t.TempDir(), cfg, tareaDesconocido, nil, DefaultTimeout)
	if res.Aprobada {
		t.Fatal("tarea con agente desconocido aprobada")
	}
	if !strings.Contains(res.PrimerMotivo(), "agente desconocido: no-existe") {
		t.Errorf("motivo = %q, quiere que contenga 'agente desconocido: no-existe'", res.PrimerMotivo())
	}
}

// listo_cuando reales. Los dos primeros son de los contratos que el
// planificador escribió para un monorepo backend + frontend: la esclusa los rechazaba con "comando no encontrado: cd" aunque
// el bucle los corre con sh -c sin problema. El tercero
// encadena varios programas.
func TestEjecutableLeeElComandoComoSh(t *testing.T) {
	for _, c := range []struct {
		cmd       string
		programas []string
	}{
		{"cd backend && pip install -e '.[dev]' -q && python -m pytest tests/test_palette.py -q", []string{"pip", "python"}},
		{"cd frontend && npm install --silent && npx vitest run src/lib/image", []string{"npm", "npx"}},
		{"node --test src/app/app.test.js && node --check src/main.js && node --check scripts/serve.js && npm test", []string{"node", "npm"}},
	} {
		for _, p := range c.programas {
			if _, err := exec.LookPath(p); err != nil {
				t.Skipf("%s no está instalado", p)
			}
		}
		tarea := tareaValida()
		tarea.ListoCuando = c.cmd
		if ch := checkEjecutable(t.TempDir(), tarea); !ch.OK {
			t.Errorf("%q: %s", c.cmd, ch.Motivo)
		}
	}
	// después del cd sigue revisando el resto: un programa que no existe
	// detrás del && ya no pasa por no ser el primer token
	tarea := tareaValida()
	tarea.ListoCuando = "cd backend && binario-que-no-existe-xyz -q 2>&1"
	if ch := checkEjecutable(t.TempDir(), tarea); ch.OK || !strings.Contains(ch.Motivo, "binario-que-no-existe-xyz") {
		t.Errorf("programa inexistente tras el cd: %+v", ch)
	}
}

// vitest y jest salen verdes si uno de los archivos pedidos no existe y
// otro sí: eso no es "ya pasa", la prueba de la tarea falta por escribir.
func TestFallaHoyNoRechazaSiFaltaLaPruebaNombrada(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(root, "frontend", "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frontend", "tests", "viejo.test.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if c := checkFallaHoy(ctx, root, "true && (true tests/nuevo.test.ts tests/viejo.test.ts)", time.Minute); !c.OK || !strings.Contains(c.Motivo, "tests/nuevo.test.ts") {
		t.Errorf("con una prueba sin escribir no puede decir que ya pasa: %+v", c)
	}
	// la ruta es relativa a un cd: basta con que exista en algún lado
	if c := checkFallaHoy(ctx, root, "cd frontend && true tests/viejo.test.ts", time.Minute); c.OK {
		t.Errorf("todas las pruebas existen y pasan: la tarea no tiene sentido: %+v", c)
	}
	if c := checkFallaHoy(ctx, root, "true", time.Minute); c.OK {
		t.Errorf("sin pruebas nombradas, verde es ya pasa: %+v", c)
	}
}
