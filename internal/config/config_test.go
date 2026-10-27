package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseFull(t *testing.T) {
	data := []byte(`
# config de ejemplo
base: main
pruebas: npm test # corre en CI
zonas_prohibidas: ["package-lock.json", "migrations/**"]
unknown_key: se ignora
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Base != "main" {
		t.Errorf("Base = %q, quiero %q", cfg.Base, "main")
	}
	if cfg.Pruebas != "npm test" {
		t.Errorf("Pruebas = %q, quiero %q", cfg.Pruebas, "npm test")
	}
	want := []string{"package-lock.json", "migrations/**"}
	if !reflect.DeepEqual(cfg.ZonasProhibidas, want) {
		t.Errorf("ZonasProhibidas = %v, quiero %v", cfg.ZonasProhibidas, want)
	}
}

func TestParseBadLine(t *testing.T) {
	_, err := Parse([]byte("esto no es clave valor\n"))
	if err == nil {
		t.Fatal("Parse debió fallar con una línea sin dos puntos")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Base:            "main",
		Pruebas:         "go test ./...",
		ZonasProhibidas: DefaultForbiddenZones(),
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg, loaded) {
		t.Errorf("roundtrip = %+v, quiero %+v", loaded, cfg)
	}
}

func TestCliIdaYVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Base: "main", Pruebas: "go test ./...", Cli: "claude"}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Cli != "claude" {
		t.Errorf("Cli = %q, quiero claude", loaded.Cli)
	}
}

func TestParseTimeoutEsclusa(t *testing.T) {
	cfg, err := Parse([]byte("timeout_esclusa: 120\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TimeoutEsclusa != 120 {
		t.Errorf("TimeoutEsclusa = %d, quiero 120", cfg.TimeoutEsclusa)
	}
	for _, bad := range []string{"0", "-5", "mucho"} {
		if _, err := Parse([]byte("timeout_esclusa: " + bad + "\n")); err == nil {
			t.Errorf("Parse debió rechazar timeout_esclusa: %s", bad)
		}
	}
}

func TestParseTimeoutAgenteYPruebas(t *testing.T) {
	cfg, err := Parse([]byte("timeout_agente: 900\ntimeout_pruebas: 600\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.TimeoutAgente != 900 {
		t.Errorf("TimeoutAgente = %d, quiero 900", cfg.TimeoutAgente)
	}
	if cfg.TimeoutPruebas != 600 {
		t.Errorf("TimeoutPruebas = %d, quiero 600", cfg.TimeoutPruebas)
	}
	for _, campo := range []string{"timeout_agente", "timeout_pruebas"} {
		for _, bad := range []string{"0", "-5", "mucho"} {
			if _, err := Parse([]byte(campo + ": " + bad + "\n")); err == nil {
				t.Errorf("Parse debió rechazar %s: %s", campo, bad)
			}
		}
	}
}

func TestParseSubagentesYPresupuesto(t *testing.T) {
	cfg, err := Parse([]byte("subagentes: 4\npresupuesto_tokens: 40000\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Subagentes != 4 {
		t.Errorf("Subagentes = %d, quiero 4", cfg.Subagentes)
	}
	if cfg.PresupuestoTokens != 40000 {
		t.Errorf("PresupuestoTokens = %d, quiero 40000", cfg.PresupuestoTokens)
	}
	if _, err := Parse([]byte("subagentes: 0\n")); err == nil {
		t.Error("subagentes: 0 debía rechazarse (mínimo 1)")
	}
	if _, err := Parse([]byte("presupuesto_tokens: -1\n")); err == nil {
		t.Error("presupuesto_tokens negativo debía rechazarse")
	}
}

func TestParsePresupuestoVentanas(t *testing.T) {
	cfg, err := Parse([]byte("presupuesto:\n  claude: { 5h: 40000, semanal: 120000 }\n  opencode: { mensual: 100000 }\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	claude := cfg.PresupuestoVentanas["claude"]
	if claude["5h"] != 40000 || claude["semanal"] != 120000 {
		t.Errorf("claude = %v, quiero 5h:40000 semanal:120000", claude)
	}
	if got := cfg.PresupuestoVentanas["opencode"]["mensual"]; got != 100000 {
		t.Errorf("opencode mensual = %d, quiero 100000", got)
	}
	// ida y vuelta
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	cargada, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cargada.PresupuestoVentanas["claude"]["5h"]; got != 40000 {
		t.Errorf("tras ida y vuelta claude 5h = %d, quiero 40000", got)
	}
	if _, err := Parse([]byte("presupuesto:\n  claude: { 5h: x }\n")); err == nil {
		t.Error("tope no numérico debía rechazarse")
	}
}

func TestModeloParaYEscalado(t *testing.T) {
	cfg := Config{Modelos: map[string]string{"liviana": "barato", "media": "medio", "pesada": "caro"}}
	if got := cfg.ModeloPara("liviana"); got != "barato" {
		t.Errorf("ModeloPara(liviana) = %q, quiero barato", got)
	}
	// peso sin mapear cae al peso por defecto (media)
	if got := cfg.ModeloPara(""); got != "medio" {
		t.Errorf("ModeloPara('') = %q, quiero el de la estrategia (medio)", got)
	}
	if got := cfg.ModeloEscalado("liviana", "barato"); got != "medio" {
		t.Errorf("ModeloEscalado(liviana) = %q, quiero medio", got)
	}
	if got := cfg.ModeloEscalado("pesada", "caro"); got != "" {
		t.Errorf("ModeloEscalado(pesada) = %q, quiero nada", got)
	}
	// escalar al mismo modelo no gasta otra invocación
	cfg2 := Config{Modelos: map[string]string{"liviana": "x", "media": "x"}}
	if got := cfg2.ModeloEscalado("liviana", "x"); got != "" {
		t.Errorf("ModeloEscalado con mismo modelo = %q, quiero nada", got)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load debió fallar sin config.yml")
	}
}

func TestExists(t *testing.T) {
	root := t.TempDir()
	if Exists(root) {
		t.Fatal("Exists = true sin .loom")
	}
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Exists(root) {
		t.Fatal("Exists = false con .loom creado")
	}
}

func TestPathsStayUnderRoot(t *testing.T) {
	root := "/proyecto"
	if Path(root) != filepath.Join(root, ".loom", "config.yml") {
		t.Errorf("Path inesperado: %s", Path(root))
	}
	if TasksDir(root) != filepath.Join(root, ".loom", "tasks") {
		t.Errorf("TasksDir inesperado: %s", TasksDir(root))
	}
}

func TestPatronesPruebaIdaYVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Base: "main", Pruebas: "go test ./...", PatronesPrueba: DefaultTestPatterns()}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.PatronesPrueba, DefaultTestPatterns()) {
		t.Errorf("PatronesPrueba = %v", loaded.PatronesPrueba)
	}
}

func TestParseProveedores(t *testing.T) {
	data := []byte(`
base: main
proveedores:
  planificador: { modelo: claude-sonnet, key_env: ANTHROPIC_API_KEY }
  ejecutor:     { modelo: glm-5.2, key_env: OPENCODE_API_KEY }
  revisor:      { modelo: deepseek-v4-flash, key_env: DEEPSEEK_API_KEY }
zonas_prohibidas: ["go.sum"]
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Proveedores) != 3 {
		t.Fatalf("Proveedores = %v · quiere 3 roles", cfg.Proveedores)
	}
	if cfg.Proveedores["planificador"].Modelo != "claude-sonnet" {
		t.Errorf("planificador = %+v", cfg.Proveedores["planificador"])
	}
	if cfg.Proveedores["ejecutor"].KeyEnv != "OPENCODE_API_KEY" {
		t.Errorf("ejecutor = %+v", cfg.Proveedores["ejecutor"])
	}
	if cfg.Proveedores["revisor"].Modelo != "deepseek-v4-flash" {
		t.Errorf("revisor = %+v", cfg.Proveedores["revisor"])
	}
}

func TestProveedoresIdaYVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Base:    "main",
		Pruebas: "go test ./...",
		Proveedores: map[string]Proveedor{
			"planificador": {Modelo: "claude-sonnet", KeyEnv: "ANTHROPIC_API_KEY"},
			"ejecutor":     {Modelo: "glm-5.2", KeyEnv: "OPENCODE_API_KEY"},
		},
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Proveedores, loaded.Proveedores) {
		t.Errorf("Proveedores = %+v, quiero %+v", loaded.Proveedores, cfg.Proveedores)
	}
}

func TestModeloRol(t *testing.T) {
	cfg := Config{Proveedores: map[string]Proveedor{
		"planificador": {Modelo: "claude-sonnet"},
	}}
	if got := ModeloRol(cfg, "planificador"); got != "claude-sonnet" {
		t.Errorf("ModeloRol(planificador) = %q", got)
	}
	if got := ModeloRol(cfg, "ejecutor"); got != "" {
		t.Errorf("ModeloRol(ejecutor) = %q, quiere vacío", got)
	}
	if got := ModeloRol(Config{}, "planificador"); got != "" {
		t.Errorf("ModeloRol sin config = %q, quiere vacío", got)
	}
}

func TestParseEstrategiaYModelos(t *testing.T) {
	data := []byte(`
estrategia: pesada
modelos:
  liviana: glm-4
  media: glm-5.2
  pesada: claude-sonnet
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Estrategia != "pesada" {
		t.Errorf("Estrategia = %q", cfg.Estrategia)
	}
	if cfg.Modelos["pesada"] != "claude-sonnet" || cfg.Modelos["liviana"] != "glm-4" {
		t.Errorf("Modelos = %v", cfg.Modelos)
	}
}

func TestModelosIdaYVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Base:       "main",
		Pruebas:    "go test ./...",
		Estrategia: "equilibrada",
		Modelos:    map[string]string{"liviana": "glm-4", "media": "glm-5.2", "pesada": "claude-sonnet"},
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Modelos, loaded.Modelos) || loaded.Estrategia != cfg.Estrategia {
		t.Errorf("roundtrip = %+v / %q", loaded.Modelos, loaded.Estrategia)
	}
}

func TestPesoPorDefecto(t *testing.T) {
	cases := map[string]string{"ligera": "liviana", "equilibrada": "media", "pesada": "pesada", "": "media"}
	for estrategia, want := range cases {
		cfg := Config{Estrategia: estrategia}
		if got := cfg.PesoPorDefecto(); got != want {
			t.Errorf("PesoPorDefecto(%q) = %q, quiero %q", estrategia, got, want)
		}
	}
}

func TestModeloPeso(t *testing.T) {
	cfg := Config{Modelos: map[string]string{"pesada": "claude-sonnet"}}
	if got := cfg.ModeloPeso("pesada"); got != "claude-sonnet" {
		t.Errorf("ModeloPeso(pesada) = %q", got)
	}
	if got := cfg.ModeloPeso("liviana"); got != "" {
		t.Errorf("ModeloPeso(liviana) = %q, quiere vacío", got)
	}
	if got := (Config{}).ModeloPeso("media"); got != "" {
		t.Errorf("ModeloPeso sin config = %q, quiere vacío", got)
	}
}

func TestParseAgentes(t *testing.T) {
	data := []byte(`
base: main
agentes:
  architect:   { provider: claude, model: claude-sonnet, key_env: ANTHROPIC_API_KEY, skills: ["diseno", "arquitectura"] }
  implementer: { provider: opencode, model: glm-5.2, key_env: OPENCODE_API_KEY, skills: ["go", "refactor"] }
  tester:      { provider: claude, model: claude-haiku, skills: ["tests", "cobertura"] }
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Agentes) != 3 {
		t.Fatalf("Agentes = %v · quiere 3 agentes", cfg.Agentes)
	}

	arch := cfg.Agentes["architect"]
	if arch.Provider != "claude" || arch.Modelo != "claude-sonnet" || arch.KeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("architect = %+v", arch)
	}
	wantArchSkills := []string{"diseno", "arquitectura"}
	if !reflect.DeepEqual(arch.Skills, wantArchSkills) {
		t.Errorf("architect.Skills = %v, quiero %v", arch.Skills, wantArchSkills)
	}

	impl := cfg.Agentes["implementer"]
	if impl.Provider != "opencode" || impl.Modelo != "glm-5.2" || impl.KeyEnv != "OPENCODE_API_KEY" {
		t.Errorf("implementer = %+v", impl)
	}
	wantImplSkills := []string{"go", "refactor"}
	if !reflect.DeepEqual(impl.Skills, wantImplSkills) {
		t.Errorf("implementer.Skills = %v, quiero %v", impl.Skills, wantImplSkills)
	}

	tester := cfg.Agentes["tester"]
	if tester.Provider != "claude" || tester.Modelo != "claude-haiku" || tester.KeyEnv != "" {
		t.Errorf("tester = %+v", tester)
	}
	wantTesterSkills := []string{"tests", "cobertura"}
	if !reflect.DeepEqual(tester.Skills, wantTesterSkills) {
		t.Errorf("tester.Skills = %v, quiero %v", tester.Skills, wantTesterSkills)
	}
}

func TestAgentesIdaYVuelta(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Base:    "main",
		Pruebas: "go test ./...",
		Agentes: map[string]Agente{
			"architect":   {Provider: "claude", Modelo: "claude-sonnet", KeyEnv: "ANTHROPIC_API_KEY", Skills: []string{"diseno"}},
			"implementer": {Provider: "opencode", Modelo: "glm-5.2", KeyEnv: "OPENCODE_API_KEY", Skills: []string{"go"}},
			"tester":      {Provider: "claude", Modelo: "claude-haiku"},
		},
	}
	if err := cfg.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(cfg.Agentes, loaded.Agentes) {
		t.Errorf("Agentes = %+v, quiero %+v", loaded.Agentes, cfg.Agentes)
	}
}

func TestConfigSinAgentes(t *testing.T) {
	data := []byte(`
base: main
pruebas: go test ./...
proveedores:
  planificador: { modelo: claude-sonnet, key_env: ANTHROPIC_API_KEY }
  ejecutor:     { modelo: glm-5.2, key_env: OPENCODE_API_KEY }
modelos:
  liviana: glm-4
  media: glm-5.2
estrategia: equilibrada
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Agentes != nil {
		t.Errorf("Agentes = %v, quiere nil", cfg.Agentes)
	}
	if len(cfg.Proveedores) != 2 {
		t.Errorf("Proveedores = %v, quiere 2", cfg.Proveedores)
	}
	if got := ModeloRol(cfg, "planificador"); got != "claude-sonnet" {
		t.Errorf("ModeloRol(planificador) = %q, quiere claude-sonnet", got)
	}
	if got := ModeloRol(cfg, "ejecutor"); got != "glm-5.2" {
		t.Errorf("ModeloRol(ejecutor) = %q, quiere glm-5.2", got)
	}
}

func TestParseAgenteProviderInvalido(t *testing.T) {
	data := []byte(`
base: main
agentes:
  architect: { provider: openai, model: gpt-4 }
`)
	_, err := Parse(data)
	if err == nil {
		t.Fatal("Parse debió fallar con un provider inválido")
	}
	want := "provider desconocido: openai · usa claude, opencode, codex"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, quiere que contenga %q", err.Error(), want)
	}
}

func TestAgentesGanaSobreProveedores(t *testing.T) {
	data := []byte(`
base: main
agentes:
  planificador: { provider: claude, model: claude-3-7-sonnet }
proveedores:
  planificador: { modelo: claude-3-5-sonnet, key_env: ANTHROPIC_API_KEY }
  ejecutor:     { modelo: glm-5.2, key_env: OPENCODE_API_KEY }
`)
	cfg, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// agentes gana para los roles que define
	if got := ModeloRol(cfg, "planificador"); got != "claude-3-7-sonnet" {
		t.Errorf("ModeloRol(planificador) = %q, quiere claude-3-7-sonnet", got)
	}
	// proveedores resuelve los roles no cubiertos por agentes
	if got := ModeloRol(cfg, "ejecutor"); got != "glm-5.2" {
		t.Errorf("ModeloRol(ejecutor) = %q, quiere glm-5.2", got)
	}
	// rol inexistente
	if got := ModeloRol(cfg, "revisor"); got != "" {
		t.Errorf("ModeloRol(revisor) = %q, quiere vacío", got)
	}
}

func TestObtenerAgente(t *testing.T) {
	cfg := Config{
		Cli: "claude",
		Agentes: map[string]Agente{
			"backend": {Provider: "opencode", Modelo: "glm-5.2", Skills: []string{"custom-backend"}},
		},
		Proveedores: map[string]Proveedor{
			"revisor": {Modelo: "claude-haiku", KeyEnv: "ANTHROPIC_API_KEY"},
		},
	}

	// 1. Agente explícito en config gana
	ag, ok := cfg.ObtenerAgente("backend")
	if !ok || ag.Provider != "opencode" || len(ag.Skills) != 1 || ag.Skills[0] != "custom-backend" {
		t.Errorf("backend = %+v, ok = %v", ag, ok)
	}

	// 2. Arquetipo por defecto (frontend) se resuelve automáticamente
	front, ok := cfg.ObtenerAgente("frontend")
	if !ok || front.Provider != "claude" || front.Modelo != "" || len(front.Skills) == 0 {
		t.Errorf("frontend = %+v, ok = %v · el arquetipo no debe traer modelo propio", front, ok)
	}

	// 3. Arquetipo architect
	arch, ok := cfg.ObtenerAgente("architect")
	if !ok || arch.Provider != "claude" || len(arch.Skills) == 0 {
		t.Errorf("architect = %+v, ok = %v", arch, ok)
	}

	// 4. Rol en Proveedores
	rev, ok := cfg.ObtenerAgente("revisor")
	if !ok || rev.Modelo != "claude-haiku" {
		t.Errorf("revisor = %+v, ok = %v", rev, ok)
	}

	// 5. Agente totalmente desconocido
	_, ok = cfg.ObtenerAgente("agente-fantasma-xyz")
	if ok {
		t.Error("agente-fantasma-xyz debió dar ok = false")
	}

	// 6. TodosLosAgentes incluye arquetipos y personalizaciones
	todos := cfg.TodosLosAgentes()
	if todos["backend"].Skills[0] != "custom-backend" {
		t.Errorf("todos backend no tomó la personalización: %+v", todos["backend"])
	}
	if _, ok := todos["frontend"]; !ok {
		t.Error("todos frontend no está presente")
	}
}

func TestAgentesPagados(t *testing.T) {
	cfg, err := Parse([]byte("agentes_pagados: 5\n"))
	if err != nil || cfg.AgentesPagados != 5 {
		t.Fatalf("%v %d", err, cfg.AgentesPagados)
	}
	if _, err := Parse([]byte("agentes_pagados: 0\n")); err == nil {
		t.Error("0 debió rechazarse")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(root); err != nil || got.AgentesPagados != 5 {
		t.Errorf("no sobrevive al guardar: %v %d", err, got.AgentesPagados)
	}
}

func TestPantallasSobrevivenAlGuardar(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".loom"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Base: "main"}
	cfg.Pantallas.Levantar = "cd frontend && npx vite --port $PORT --host 127.0.0.1"
	cfg.Pantallas.URL = "http://127.0.0.1:$PORT"
	cfg.Pantallas.Rutas = []string{"/", "/#/agregar"}
	cfg.Pantallas.Semilla = "python3 scripts/semilla.py"
	cfg.Pantallas.Script = "node scripts/capturas.mjs"
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pantallas.Levantar != cfg.Pantallas.Levantar || got.Pantallas.URL != cfg.Pantallas.URL || len(got.Pantallas.Rutas) != 2 || got.Pantallas.Rutas[1] != "/#/agregar" || got.Pantallas.Semilla != cfg.Pantallas.Semilla || got.Pantallas.Script != cfg.Pantallas.Script {
		t.Fatalf("%+v", got.Pantallas)
	}
}

func TestArquitectoEconomicoVaYVuelve(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Base: "main", ArquitectoEconomico: true}
	if err := cfg.Save(root); err != nil {
		t.Fatal(err)
	}
	leida, err := Load(root)
	if err != nil || !leida.ArquitectoEconomico {
		t.Fatalf("economico=%v err=%v", leida.ArquitectoEconomico, err)
	}
}

func TestLanguageYResumeBelowPct(t *testing.T) {
	c, err := Parse([]byte("base: main\nlanguage: es\nresume_below_pct: 50\n"))
	if err != nil || c.Language != "es" || c.ResumeBelow() != 50 {
		t.Fatalf("cfg = %+v, err = %v", c, err)
	}
	if d, _ := Parse([]byte("base: main\n")); d.ResumeBelow() != DefaultResumeBelowPct || d.Language != "" {
		t.Errorf("defaults = %+v", d)
	}
	if _, err := Parse([]byte("language: fr\n")); err == nil {
		t.Error("language desconocido debe fallar")
	}
}

func TestMigrarLegadoMueveDevcleanALoom(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".devclean", "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devclean", "config.yml"), []byte("base: main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Exists(root) {
		t.Fatal("Exists debe migrar .devclean a .loom")
	}
	if _, err := os.Stat(filepath.Join(root, ".devclean")); !os.IsNotExist(err) {
		t.Error(".devclean debe haberse movido")
	}
	if _, err := os.Stat(filepath.Join(Dir(root), "config.yml")); err != nil {
		t.Errorf("config.yml no llegó a .loom: %v", err)
	}
	// con .loom ya presente, un .devclean suelto no se toca
	_ = os.MkdirAll(filepath.Join(root, ".devclean"), 0o755)
	MigrarLegado(root)
	if _, err := os.Stat(filepath.Join(root, ".devclean")); err != nil {
		t.Error("no debe pisar un .loom existente")
	}
}

func TestResumeBelowCeroEsElDefaultYOffApaga(t *testing.T) {
	if c, _ := Parse([]byte("base: main\nresume_below_pct: 0\n")); c.ResumeBelow() != DefaultResumeBelowPct {
		t.Error("0 es el default: un config.yml viejo con 0 no debe apagar --resume")
	}
	if c, err := Parse([]byte("base: main\nresume_below_pct: off\n")); err != nil || c.ResumeBelow() != 0 {
		t.Errorf("off apaga: %v %d", err, c.ResumeBelow())
	}
}

func TestConfigSeEscribeEnInglesYSeLeenAmbosNombres(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	c := Config{Base: "main", Pruebas: "go test ./...", Cli: "claude", TimeoutAgente: 600,
		Modelos:             map[string]string{"liviana": "haiku", "media": "sonnet", "pesada": "opus"},
		PresupuestoVentanas: map[string]map[string]int{"claude": {"5h": 100, "semanal": 500}},
		Proveedores:         map[string]Proveedor{"planificador": {Modelo: "x", KeyEnv: "K"}}}
	if err := c.Save(root); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(Path(root))
	txt := string(b)
	for _, quiero := range []string{"tests: go test", "agent_timeout: 600", "models:", "  light:", "  heavy:", "budget:", "weekly:", "providers:", "  planner:", "model:"} {
		if !strings.Contains(txt, quiero) {
			t.Errorf("config sin %q:\n%s", quiero, txt)
		}
	}
	for _, viejo := range []string{"pruebas:", "timeout_agente", "modelos:", "liviana", "semanal", "proveedores:"} {
		if strings.Contains(txt, viejo) {
			t.Errorf("el config todavía trae %q:\n%s", viejo, txt)
		}
	}
	got, err := Load(root)
	if err != nil || got.Pruebas != "go test ./..." || got.Modelos["liviana"] != "haiku" || got.PresupuestoVentanas["claude"]["semanal"] != 500 || got.Proveedores["planificador"].Modelo != "x" {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	// un config escrito con los nombres en español sigue leyéndose
	viejo := "base: main\npruebas: make test\nmodelos:\n  media: sonnet\n"
	if g, err := Parse([]byte(viejo)); err != nil || g.Pruebas != "make test" || g.Modelos["media"] != "sonnet" {
		t.Errorf("español: %v %+v", err, g)
	}
}

func TestLanguageSeEscribeExplicito(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(Dir(root), 0o755)
	for _, l := range []string{"en", "es"} {
		if err := (Config{Base: "main", Language: l}).Save(root); err != nil {
			t.Fatal(err)
		}
		if g, err := Load(root); err != nil || g.Language != l {
			t.Errorf("language %s: %v %+v", l, err, g)
		}
	}
	if err := (Config{Base: "main"}).Save(root); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(Path(root)); strings.Contains(string(b), "language") {
		t.Error("sin idioma elegido no se escribe la clave")
	}
}

func TestRedteamEnConfig(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(Dir(root), 0o755)
	for _, m := range []string{"warn", "block"} {
		if err := (Config{Base: "main", Redteam: m}).Save(root); err != nil {
			t.Fatal(err)
		}
		if g, err := Load(root); err != nil || g.Redteam != m {
			t.Errorf("redteam %s: %v %+v", m, err, g)
		}
	}
	if _, err := Parse([]byte("redteam: siempre\n")); err == nil {
		t.Error("un modo desconocido debe fallar")
	}
	if g, _ := Parse([]byte("base: main\n")); g.Redteam != "" {
		t.Error("por defecto no hay redteam")
	}
}

func TestModeloConstructorYTechLead(t *testing.T) {
	c := Config{Modelos: map[string]string{"liviana": "haiku", "media": "sonnet", "pesada": "opus"}}
	for peso, quiero := range map[string]string{"liviana": "haiku", "media": "sonnet", "pesada": "sonnet"} {
		if got := c.ModeloConstructor(peso); got != quiero {
			t.Errorf("constructor %s = %q, quiero %q", peso, got, quiero)
		}
	}
	if got := ModeloRol(c, "planificador"); got != "opus" {
		t.Errorf("planificador = %q, quiero opus", got)
	}
	if got := ModeloRol(c, "techlead"); got != "sonnet" {
		t.Errorf("techlead = %q, quiero sonnet", got)
	}
	c.HeavyBuilder = "opus"
	if got := c.ModeloConstructor("pesada"); got != "opus" {
		t.Errorf("heavy_builder = %q, quiero opus", got)
	}
	c2, err := Parse([]byte("heavy_builder: sonnet\n"))
	if err != nil || c2.HeavyBuilder != "sonnet" {
		t.Errorf("heavy_builder no se lee: %v %q", err, c2.HeavyBuilder)
	}
}
