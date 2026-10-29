// Package config handles .loom/config.yml, the project settings of
// loom, and the layout of the .loom directory.
//
// The file uses a small yaml subset: flat `clave: valor` scalars and
// inline lists `clave: ["a", "b"]`. Values with commas inside quotes are
// not supported. Provider and key settings arrive with the agent engine.
package config

import (
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Pastranauwu/loom/internal/capturas"
	"github.com/Pastranauwu/loom/internal/kv"
	"github.com/Pastranauwu/loom/internal/skills"
)

// DirName is the directory loom creates at the repository root.
const DirName = ".loom"

// EntregaLocal es el valor de `entrega:` que deja el PR en el repo.
const EntregaLocal = "local"

// Config is the content of .loom/config.yml.
type Config struct {
	Base    string `json:"base"`
	Pruebas string `json:"pruebas"`
	// Cli fija el CLI de agente por defecto (opencode | claude). Se llama
	// "cli" y no "ejecutor" a propósito: ese nombre ya lo usa el rol
	// `ejecutor` dentro de `proveedores`, y kv.Pairs no distingue
	// indentación — dos claves iguales a distinta profundidad se pisan.
	Cli string `json:"cli,omitempty"`
	// Entrega fija a dónde va el PR: "local" deja la rama y su descripción
	// en el repo aunque haya origin; "remoto" o vacío sube y abre el PR
	// cuando hay origin.
	Entrega string `json:"entrega,omitempty"`
	// Sandbox es auto (vacío: bwrap si sirve) u off: las pruebas corren sin aislar.
	Sandbox         string   `json:"sandbox,omitempty"`
	ZonasProhibidas []string `json:"zonas_prohibidas"`
	// DNA names the design archetypes (internal/dna) injected into prompts.
	DNA            []string `json:"dna,omitempty"`
	TimeoutEsclusa int      `json:"timeout_esclusa"` // segundos para el chequeo "falla hoy"
	// TimeoutAgente y TimeoutPruebas cubren el bucle real y la esclusa de
	// salida, no la de entrada. Sin esto, ambos caían en el default de 5
	// minutos: corto para una tarea real de agente y causa habitual de
	// "se agotaron los intentos" sin que el agente llegara a terminar.
	TimeoutAgente  int `json:"timeout_agente,omitempty"`  // segundos por invocación del agente
	TimeoutPruebas int `json:"timeout_pruebas,omitempty"` // segundos por corrida de listo_cuando/pruebas
	// AgentesPagados es cuántas tareas con modelo de pago corren a la vez
	// cuando no se pasa --agentes. 0 = el default de run (3).
	AgentesPagados int `json:"agentes_pagados,omitempty"`
	// TopeArquitectoUSD corta cada invocación del arquitecto al pasar ese
	// gasto en dólares, a precio de lista. 0 = sin tope. Solo claude.
	TopeArquitectoUSD int `json:"tope_arquitecto_usd,omitempty"`
	// ArquitectoEconomico hace que el esqueleto de un cambio sobre un
	// proyecto que ya tiene ARCHITECTURE.md lo intente primero el modelo
	// medio; si no pasa la verificación, sube al planificador. Apagado
	// por defecto: la verificación mira la forma del plano, no si el
	// reparto es bueno, y eso todavía no está medido.
	ArquitectoEconomico bool `json:"arquitecto_economico,omitempty"`
	// RecursionMax es la profundidad máxima de recursión (internal/recurse):
	// 0 (default) = desactivada, una tarea `recursivo: true` corre plana.
	// Cada nivel abre cuartos anidados dentro del cuarto actual — subirlo
	// multiplica el gasto de tokens, así que el default es apagado.
	RecursionMax int `json:"recursion_max,omitempty"`
	// Subagentes es cuántas subtareas de una misma tarea recursiva
	// corren en paralelo dentro de su cuarto padre. 1 = serial. Sin esto,
	// la recursión ejecutaba una hoja a la vez: para "muchos chicos a la
	// vez" las hojas independientes necesitan un tope propio, distinto de
	// --agentes (que limita tareas raíz en paralelo).
	Subagentes int `json:"subagentes,omitempty"`
	// PresupuestoTokens es el tope de gasto de una corrida completa
	// (suma de entrada+salida de todos los intentos, incluidas las
	// subtareas de la recursión). 0 = sin tope. Al pasarlo, la corrida se
	// corta con un motivo legible; board/run muestran cuánto va y cuánto
	// queda.
	PresupuestoTokens int `json:"presupuesto_tokens,omitempty"`
	// ResumeBelowPct: while a claude session uses less than this % of its
	// context window, the next attempt continues it with a short prompt.
	// 0 = the default (70), negative = never resume (`off` in the file).
	ResumeBelowPct int `json:"resume_below_pct"`
	// Redteam is off (default), warn or block: whether a saboteur attacks a task
	// before it ships, and whether a confirmed crash stops the PR.
	Redteam string `json:"redteam,omitempty"`
	// HeavyBuilder is the model of builders on heavy tasks. Empty = the
	// medium model: the heavy one is for the architect and heal.
	HeavyBuilder string `json:"heavy_builder,omitempty"`
	// EscalarTras es cuántos intentos tiene el modelo barato antes de
	// subir un escalón. 0 = DefaultEscalarTras; -1 (off) = las tareas
	// medias arrancan en el modelo medio y escalar espera a que se
	// agoten los intentos, como antes.
	EscalarTras int `json:"escalar_tras,omitempty"`
	// Language is the language of prose written for people: "en" (default) or "es".
	Language string `json:"language,omitempty"`
	// PresupuestoVentanas es el bloque `presupuesto:`: por proveedor, por
	// ventana rodante (5h/semanal/mensual), cuántos tokens puede quemar
	// loom. El ledger de internal/ventanas lo hace cumplir y lo
	// muestra en barras; protege las ventanas reales de la cuenta sin
	// depender de que el proveedor las exponga.
	PresupuestoVentanas map[string]map[string]int `json:"presupuesto_ventanas,omitempty"`
	PatronesPrueba      []string                  `json:"patrones_prueba"` // rutas que ninguna tarea puede editar
	Proveedores         map[string]Proveedor      `json:"proveedores,omitempty"`
	Agentes             map[string]Agente         `json:"agentes,omitempty"`
	Estrategia          string                    `json:"estrategia,omitempty"` // ligera | equilibrada | pesada
	Modelos             map[string]string         `json:"modelos,omitempty"`    // peso -> modelo (liviana/media/pesada)
	// ReglasImport declara la dirección permitida entre módulos:
	// cada string es una cadena como "api → dominio → datos". Se verifica
	// en la esclusa de salida que ningún import del diff viole el orden.
	ReglasImport []string `json:"reglas_import,omitempty"`
	// Pantallas dice cómo levantar la interfaz web y qué capturar. La
	// declara el arquitecto; vacía = el proyecto no tiene web o no se
	// sabe levantarla, y no hay capturas.
	Pantallas capturas.Pantallas `json:"pantallas,omitempty"`
}

// Proveedor es un rol del motor de agentes: qué modelo usa y de
// qué variable de entorno sale su key. Los roles son planificador,
// ejecutor y revisor.
type Proveedor struct {
	Modelo string `json:"modelo"`
	KeyEnv string `json:"key_env"`
}

// Agente es un agente del motor: qué proveedor usa, qué
// modelo, variable de entorno para su API key y habilidades asociadas.
type Agente struct {
	Provider string   `json:"provider"`
	Modelo   string   `json:"model"`
	KeyEnv   string   `json:"key_env,omitempty"`
	Skills   []string `json:"skills,omitempty"`
	// SkillPackages son nombres de paquetes de skill reales (traídos con
	// `loom skills sync`, ver internal/skills): a diferencia de
	// Skills, que son solo etiquetas descriptivas en el prompt, estos
	// resuelven a un SKILL.md cuyo contenido completo se inyecta.
	SkillPackages []string `json:"skill_packages,omitempty"`
}

// DefaultForbiddenZones lists the global forbidden zones: lockfiles, migrations, CI and
// changelog never belong to a task.
func DefaultForbiddenZones() []string {
	return []string{
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"poetry.lock",
		"Pipfile.lock",
		"go.sum",
		"Cargo.lock",
		"migrations/**",
		".github/**",
		"CHANGELOG*",
	}
}

// lockfilePorManifiesto empareja cada manifiesto de dependencias con el
// archivo de bloqueo que el gestor regenera a partir de él.
var lockfilePorManifiesto = map[string][]string{
	"go.mod":         {"go.sum"},
	"package.json":   {"package-lock.json", "yarn.lock", "pnpm-lock.yaml"},
	"Cargo.toml":     {"Cargo.lock"},
	"pyproject.toml": {"poetry.lock"},
	"Pipfile":        {"Pipfile.lock"},
}

// LockfilesDerivados devuelve los archivos de bloqueo que la herramienta
// regenera a partir de los manifiestos presentes en un alcance.
//
// Los lockfiles son zona prohibida para que nadie los edite a mano
// y finja una resolución de dependencias. Pero cuando la tarea sí puede
// tocar el manifiesto, prohibir el lockfile deja el proyecto sin compilar:
// añadir una dependencia a go.mod y revertir go.sum daba
// "missing go.sum entry" en cada intento, para siempre. Derivado del
// manifiesto que la tarea ya tiene en alcance, el lockfile va con él.
func LockfilesDerivados(tocarSolo []string) []string {
	var out []string
	visto := map[string]bool{}
	for _, p := range tocarSolo {
		for _, lock := range lockfilePorManifiesto[path.Base(p)] {
			if !visto[lock] {
				visto[lock] = true
				out = append(out, lock)
			}
		}
	}
	return out
}

// DefaultTestPatterns are the test paths no contract may claim: las
// escribe un examinador ciego, así que el implementador nunca puede
// editarlas.
func DefaultTestPatterns() []string {
	return []string{
		"*_test.go",
		"test/**",
		"tests/**",
		"spec/**",
		"*.spec.ts",
		"*.test.ts",
		"test_*.py",
	}
}

// Dir returns the .loom directory of the repository at root.
func Dir(root string) string { return filepath.Join(root, DirName) }

// Path returns the config.yml path of the repository at root.
func Path(root string) string { return filepath.Join(Dir(root), "config.yml") }

// TasksDir returns the tasks directory of the repository at root.
func TasksDir(root string) string { return filepath.Join(Dir(root), "tasks") }

// Exists reports whether root already holds a .loom directory.
func Exists(root string) bool {
	MigrarLegado(root)
	info, err := os.Stat(Dir(root))
	return err == nil && info.IsDir()
}

// LegacyDirName is the directory devclean used before the rename to loom.
const LegacyDirName = ".devclean"

// MigrarLegado moves a project's .devclean/ to .loom/ the first time loom
// sees it. Rooms created inside it are git worktrees, so their links are
// repaired afterwards. It does nothing if .loom/ exists or there is no
// .devclean/, and reports nothing on failure: Exists then says no.
func MigrarLegado(root string) {
	if _, err := os.Stat(Dir(root)); err == nil {
		return
	}
	viejo := filepath.Join(root, LegacyDirName)
	if info, err := os.Stat(viejo); err != nil || !info.IsDir() {
		return
	}
	if os.Rename(viejo, Dir(root)) != nil {
		return
	}
	cuartos, _ := os.ReadDir(filepath.Join(Dir(root), "rooms"))
	if len(cuartos) == 0 {
		return
	}
	args := []string{"-C", root, "worktree", "repair"}
	for _, c := range cuartos {
		args = append(args, filepath.Join(Dir(root), "rooms", c.Name()))
	}
	_ = exec.Command("git", args...).Run()
}

// Load reads .loom/config.yml under root.
func Load(root string) (Config, error) {
	data, err := os.ReadFile(Path(root))
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, errors.New(idioma.T("sin configuración · corre loom init primero"))
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := Parse(data)
	if err == nil {
		idioma.Set(cfg.Language)
	}
	return cfg, err
}

// Save writes the config to .loom/config.yml under root.
func (c Config) Save(root string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "base: %s\n", c.Base)
	fmt.Fprintf(&b, "pruebas: %s\n", c.Pruebas)
	if !c.Pantallas.Vacia() {
		fmt.Fprintf(&b, "pantallas_levantar: %s\n", kv.Quote(c.Pantallas.Levantar))
		fmt.Fprintf(&b, "pantallas_url: %s\n", kv.Quote(c.Pantallas.URL))
		fmt.Fprintf(&b, "pantallas_rutas: %s\n", kv.MarshalList(c.Pantallas.Rutas))
		if c.Pantallas.Semilla != "" {
			fmt.Fprintf(&b, "pantallas_semilla: %s\n", kv.Quote(c.Pantallas.Semilla))
		}
		if c.Pantallas.Script != "" {
			fmt.Fprintf(&b, "pantallas_script: %s\n", kv.Quote(c.Pantallas.Script))
		}
		if c.Pantallas.Muestra != "" {
			fmt.Fprintf(&b, "pantallas_muestra: %s\n", kv.Quote(c.Pantallas.Muestra))
		}
		if c.Pantallas.Max > 0 {
			fmt.Fprintf(&b, "pantallas_max: %d\n", c.Pantallas.Max)
		}
	}
	if c.EscalarTras != 0 {
		v := strconv.Itoa(c.EscalarTras)
		if c.EscalarTras < 0 {
			v = "off"
		}
		fmt.Fprintf(&b, "escalar_tras: %s\n", v)
	}
	if c.Cli != "" {
		fmt.Fprintf(&b, "cli: %s\n", c.Cli)
	}
	if c.Sandbox != "" {
		fmt.Fprintf(&b, "aislamiento: %s\n", c.Sandbox)
	}
	if c.Entrega != "" {
		fmt.Fprintf(&b, "entrega: %s\n", c.Entrega)
	}
	fmt.Fprintf(&b, "zonas_prohibidas: %s\n", kv.MarshalList(c.ZonasProhibidas))
	if len(c.DNA) > 0 {
		fmt.Fprintf(&b, "dna: %s\n", kv.MarshalList(c.DNA))
	}
	if len(c.PatronesPrueba) > 0 {
		fmt.Fprintf(&b, "patrones_prueba: %s\n", kv.MarshalList(c.PatronesPrueba))
	}
	if c.TimeoutEsclusa > 0 {
		fmt.Fprintf(&b, "timeout_esclusa: %d\n", c.TimeoutEsclusa)
	}
	if c.TimeoutAgente > 0 {
		fmt.Fprintf(&b, "timeout_agente: %d\n", c.TimeoutAgente)
	}
	if c.TimeoutPruebas > 0 {
		fmt.Fprintf(&b, "timeout_pruebas: %d\n", c.TimeoutPruebas)
	}
	if c.ArquitectoEconomico {
		fmt.Fprintf(&b, "arquitecto_economico: true\n")
	}
	if c.AgentesPagados > 0 {
		fmt.Fprintf(&b, "agentes_pagados: %d\n", c.AgentesPagados)
	}
	if c.TopeArquitectoUSD > 0 {
		fmt.Fprintf(&b, "tope_arquitecto_usd: %d\n", c.TopeArquitectoUSD)
	}
	if c.RecursionMax > 0 {
		fmt.Fprintf(&b, "recursion_max: %d\n", c.RecursionMax)
	}
	if c.Subagentes > 0 {
		fmt.Fprintf(&b, "subagentes: %d\n", c.Subagentes)
	}
	if c.PresupuestoTokens > 0 {
		fmt.Fprintf(&b, "presupuesto_tokens: %d\n", c.PresupuestoTokens)
	}
	if c.Redteam == "warn" || c.Redteam == "block" {
		fmt.Fprintf(&b, "redteam: %s\n", c.Redteam)
	}
	if c.HeavyBuilder != "" {
		fmt.Fprintf(&b, "heavy_builder: %s\n", c.HeavyBuilder)
	}
	if c.Language == "en" || c.Language == "es" {
		fmt.Fprintf(&b, "language: %s\n", c.Language)
	}
	if c.ResumeBelowPct < 0 {
		b.WriteString("resume_below_pct: off\n")
	} else if c.ResumeBelowPct > 0 && c.ResumeBelowPct != DefaultResumeBelowPct {
		fmt.Fprintf(&b, "resume_below_pct: %d\n", c.ResumeBelowPct)
	}
	if len(c.Proveedores) > 0 {
		fmt.Fprintf(&b, "proveedores:\n")
		for _, rol := range sortedRoles(c.Proveedores) {
			p := c.Proveedores[rol]
			fmt.Fprintf(&b, "  %s: { modelo: %s, key_env: %s }\n", rol, kv.Quote(p.Modelo), kv.Quote(p.KeyEnv))
		}
	}
	if len(c.Agentes) > 0 {
		fmt.Fprintf(&b, "agentes:\n")
		for _, nombre := range sortedAgentNames(c.Agentes) {
			a := c.Agentes[nombre]
			var parts []string
			if a.Provider != "" {
				parts = append(parts, fmt.Sprintf("provider: %s", kv.Quote(a.Provider)))
			}
			if a.Modelo != "" {
				parts = append(parts, fmt.Sprintf("model: %s", kv.Quote(a.Modelo)))
			}
			if a.KeyEnv != "" {
				parts = append(parts, fmt.Sprintf("key_env: %s", kv.Quote(a.KeyEnv)))
			}
			if len(a.Skills) > 0 {
				parts = append(parts, fmt.Sprintf("skills: %s", kv.MarshalList(a.Skills)))
			}
			if len(a.SkillPackages) > 0 {
				parts = append(parts, fmt.Sprintf("skill_packages: %s", kv.MarshalList(a.SkillPackages)))
			}
			fmt.Fprintf(&b, "  %s: { %s }\n", nombre, strings.Join(parts, ", "))
		}
	}
	if c.Estrategia != "" {
		fmt.Fprintf(&b, "estrategia: %s\n", c.Estrategia)
	}
	if len(c.ReglasImport) > 0 {
		fmt.Fprintf(&b, "reglas_import: %s\n", kv.MarshalList(c.ReglasImport))
	}
	if len(c.Modelos) > 0 {
		fmt.Fprintf(&b, "modelos:\n")
		for _, peso := range sortedStringKeys(c.Modelos) {
			fmt.Fprintf(&b, "  %s: %s\n", peso, kv.Quote(c.Modelos[peso]))
		}
	}
	if len(c.PresupuestoVentanas) > 0 {
		fmt.Fprintf(&b, "presupuesto:\n")
		for _, proveedor := range sortedPresupuestoKeys(c.PresupuestoVentanas) {
			var partes []string
			for _, ventana := range []string{"5h", "semanal", "mensual"} {
				if n := c.PresupuestoVentanas[proveedor][ventana]; n > 0 {
					partes = append(partes, ventana+": "+strconv.Itoa(n))
				}
			}
			if len(partes) > 0 {
				fmt.Fprintf(&b, "  %s: { %s }\n", proveedor, strings.Join(partes, ", "))
			}
		}
	}
	return os.WriteFile(Path(root), []byte(aIngles(b.String())), 0o644)
}

// KeyEnvDe devuelve la variable de entorno donde vive la key de un
// proveedor (claude, opencode, o un rol), buscando primero en agentes y
// luego en proveedores. Vacío si no está declarada.
func (c Config) KeyEnvDe(nombre string) string {
	if a, ok := c.Agentes[nombre]; ok && a.KeyEnv != "" {
		return a.KeyEnv
	}
	if c.Proveedores != nil {
		if p, ok := c.Proveedores[nombre]; ok {
			return p.KeyEnv
		}
	}
	return ""
}

// sortedStringKeys devuelve las claves de un mapa en orden estable.
func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedRoles devuelve los roles en orden estable para un Save
// determinista.
func sortedRoles(m map[string]Proveedor) []string {
	roles := make([]string, 0, len(m))
	for r := range m {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	return roles
}

// sortedPresupuestoKeys devuelve los proveedores del bloque presupuesto
// en orden estable.
func sortedPresupuestoKeys(m map[string]map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedAgentNames devuelve los nombres de agentes en orden estable para Save.
func sortedAgentNames(m map[string]Agente) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ModeloRol devuelve el modelo declarado para un rol (planificador,
// ejecutor, revisor, examinador) en la configuración, o el que el rol
// pide por defecto, o "" si no hay nada declarado. Si existe un agente
// con ese nombre en `agentes:`, su modelo tiene prioridad sobre
// `proveedores:`.
//
// El default de cada rol sigue el principio de no quemar el modelo de
// frontera en trabajo repetitivo: planificador (arquitectura y
// decisiones) cae al modelo pesado; revisor y examinador (juicio y
// pruebas por tarea, corren muchas veces por corrida) caen a modelos
// baratos. Quien quiera otra cosa, lo escribe en config.yml.
func ModeloRol(c Config, rol string) string {
	if a, ok := c.Agentes[rol]; ok && a.Modelo != "" {
		return a.Modelo
	}
	if m := c.Proveedores[rol].Modelo; m != "" {
		return m
	}
	switch rol {
	case "planificador":
		return c.ModeloPeso("pesada")
	case "revisor", "techlead":
		return c.ModeloPeso("media")
	case "examinador":
		return c.ModeloPeso("liviana")
	}
	return ""
}

// PesoPorDefecto devuelve el peso que usa una tarea sin peso explícito,
// a partir de la estrategia global. Por defecto, media.
func (c Config) PesoPorDefecto() string {
	switch c.Estrategia {
	case "ligera":
		return "liviana"
	case "pesada":
		return "pesada"
	default:
		return "media"
	}
}

// ModeloPeso devuelve el modelo asociado a un peso (liviana/media/pesada)
// en la configuración, o "" si no está declarado.
func (c Config) ModeloPeso(peso string) string {
	if c.Modelos == nil {
		return ""
	}
	return c.Modelos[peso]
}

// ModeloConstructor is the model a builder runs a task of that weight on.
// Only the architect, the planner and heal use the heavy model: a builder
// on a heavy task gets the medium one unless heavy_builder says otherwise.
func (c Config) ModeloConstructor(peso string) string {
	peso = c.PesoInicial(peso)
	if peso == "pesada" {
		if c.HeavyBuilder != "" {
			return c.HeavyBuilder
		}
		if m := c.ModeloPeso("media"); m != "" {
			return m
		}
	}
	return c.ModeloPeso(peso)
}

// ModeloPara devuelve el modelo de un peso de tarea, cayendo al peso por
// defecto de la estrategia si el peso pedido no está mapeado, y "" si no
// hay nada declarado (el CLI decide entonces).
func (c Config) ModeloPara(peso string) string {
	if peso == "" {
		peso = c.PesoPorDefecto()
	}
	if m := c.ModeloConstructor(peso); m != "" {
		return m
	}
	return c.ModeloPeso(c.PesoPorDefecto())
}

// DefaultEscalarTras: dos intentos del barato y, si no sale, sube.
const DefaultEscalarTras = 2

// Ahorro reporta si las tareas arrancan un escalón abajo y escalan tras
// EscalarTras intentos: haiku primero, sonnet si no sale.
func (c Config) Ahorro() bool { return c.EscalarTras >= 0 }

// IntentosAntesDeEscalar es el tope de intentos del modelo barato, o 0
// si el ahorro está apagado.
func (c Config) IntentosAntesDeEscalar() int {
	switch {
	case c.EscalarTras < 0:
		return 0
	case c.EscalarTras == 0:
		return DefaultEscalarTras
	}
	return c.EscalarTras
}

// PesoInicial es el peso con que arranca el constructor: con ahorro, una
// tarea media empieza en el modelo liviano y escala al medio si no sale.
// La pesada no baja: ya corre en el medio (ModeloConstructor).
func (c Config) PesoInicial(peso string) string {
	if peso == "" {
		peso = c.PesoPorDefecto()
	}
	if peso == "media" && c.Ahorro() && c.ModeloPeso("liviana") != "" {
		return "liviana"
	}
	return peso
}

// ModeloEscalado devuelve el modelo del peso siguiente al dado
// (liviana→media), o "" si no hay, si es el mismo que `actual` o si no
// está mapeado. Es el paso del fallback: el barato falló con trabajo, se
// sube un escalón sin tocar el contrato.
//
// Nunca escala a pesada: opus es lo que vacía la ventana de 5 h, y una
// tarea que ni el medio resuelve casi siempre tiene el contrato mal
// hecho, no le falta modelo. Una tarea con peso pesada sí usa opus.
func (c Config) ModeloEscalado(peso, actual string) string {
	peso = c.PesoInicial(peso)
	siguiente := ""
	for i, p := range Pesos {
		if p == peso && i+1 < len(Pesos) {
			siguiente = Pesos[i+1]
			break
		}
	}
	if siguiente == "" || siguiente == "pesada" {
		return ""
	}
	m := c.ModeloPeso(siguiente)
	if m == "" || m == actual {
		return ""
	}
	return m
}

// Parse reads the config yaml subset. Unknown keys are ignored.
func Parse(data []byte) (Config, error) {
	data = []byte(aEspanol(string(data)))
	cfg := Config{}
	pares, err := kv.Pairs(strings.Split(string(data), "\n"), 1)
	if err != nil {
		return cfg, fmt.Errorf(idioma.T("config.yml: %s"), err)
	}
	for _, p := range pares {
		switch p.Key {
		case "base":
			cfg.Base = kv.Unquote(p.Value)
		case "pruebas":
			cfg.Pruebas = kv.Unquote(p.Value)
		case "cli":
			cfg.Cli = kv.Unquote(p.Value)
		case "aislamiento":
			cfg.Sandbox = kv.Unquote(p.Value)
			if cfg.Sandbox != "auto" && cfg.Sandbox != "off" {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · sandbox es auto u off, no %q"), p.Line, cfg.Sandbox)
			}
		case "entrega":
			cfg.Entrega = kv.Unquote(p.Value)
			if cfg.Entrega != EntregaLocal && cfg.Entrega != "remoto" {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · entrega es local o remoto, no %q"), p.Line, cfg.Entrega)
			}
		case "dna":
			cfg.DNA = listaFlexible(p.Value)
		case "zonas_prohibidas":
			list, err := kv.ParseList(p.Value)
			if err != nil {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), p.Line, err)
			}
			cfg.ZonasProhibidas = list
		case "pantallas_levantar":
			cfg.Pantallas.Levantar = kv.Unquote(p.Value)
		case "pantallas_semilla":
			cfg.Pantallas.Semilla = kv.Unquote(p.Value)
		case "pantallas_script":
			cfg.Pantallas.Script = kv.Unquote(p.Value)
		case "pantallas_url":
			cfg.Pantallas.URL = kv.Unquote(p.Value)
		case "pantallas_muestra":
			cfg.Pantallas.Muestra = kv.Unquote(p.Value)
		case "pantallas_max":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · pantallas_max inválido: %s · entero, mínimo 1"), p.Line, p.Value)
			}
			cfg.Pantallas.Max = n
		case "escalar_tras":
			if v := kv.Unquote(p.Value); v == "off" || v == "0" {
				cfg.EscalarTras = -1
				continue
			}
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · escalar_tras inválido: %s · intentos con el modelo barato antes de subir, o off"), p.Line, p.Value)
			}
			cfg.EscalarTras = n
		case "pantallas_rutas":
			list, err := kv.ParseList(p.Value)
			if err != nil {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), p.Line, err)
			}
			cfg.Pantallas.Rutas = list
		case "patrones_prueba":
			list, err := kv.ParseList(p.Value)
			if err != nil {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), p.Line, err)
			}
			cfg.PatronesPrueba = list
		case "timeout_esclusa":
			seg, err := kv.ParseInt(p.Value)
			if err != nil || seg < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · timeout_esclusa inválido: %s · segundos, mínimo 1"), p.Line, p.Value)
			}
			cfg.TimeoutEsclusa = seg
		case "arquitecto_economico":
			switch kv.Unquote(p.Value) {
			case "true":
				cfg.ArquitectoEconomico = true
			case "false":
				cfg.ArquitectoEconomico = false
			default:
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · arquitecto_economico inválido: %s · usa true o false"), p.Line, p.Value)
			}
		case "agentes_pagados":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · agentes_pagados inválido: %s · mínimo 1"), p.Line, p.Value)
			}
			cfg.AgentesPagados = n
		case "tope_arquitecto_usd":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · tope_arquitecto_usd inválido: %s · dólares enteros, mínimo 1"), p.Line, p.Value)
			}
			cfg.TopeArquitectoUSD = n
		case "timeout_agente":
			seg, err := kv.ParseInt(p.Value)
			if err != nil || seg < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · timeout_agente inválido: %s · segundos, mínimo 1"), p.Line, p.Value)
			}
			cfg.TimeoutAgente = seg
		case "timeout_pruebas":
			seg, err := kv.ParseInt(p.Value)
			if err != nil || seg < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · timeout_pruebas inválido: %s · segundos, mínimo 1"), p.Line, p.Value)
			}
			cfg.TimeoutPruebas = seg
		case "recursion_max":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 0 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · recursion_max inválido: %s · entero, mínimo 0"), p.Line, p.Value)
			}
			cfg.RecursionMax = n
		case "subagentes":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 1 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · subagentes inválido: %s · entero, mínimo 1"), p.Line, p.Value)
			}
			cfg.Subagentes = n
		case "presupuesto_tokens":
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 0 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · presupuesto_tokens inválido: %s · entero, mínimo 0 (0 = sin tope)"), p.Line, p.Value)
			}
			cfg.PresupuestoTokens = n
		case "heavy_builder":
			cfg.HeavyBuilder = kv.Unquote(p.Value)
		case "redteam":
			v := kv.Unquote(p.Value)
			if v != "off" && v != "warn" && v != "block" {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · redteam inválido: %s · usa off, warn o block"), p.Line, p.Value)
			}
			cfg.Redteam = v
		case "language":
			l := kv.Unquote(p.Value)
			if l != "en" && l != "es" {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · language inválido: %s · usa en o es"), p.Line, p.Value)
			}
			cfg.Language = l
		case "resume_below_pct":
			if v := kv.Unquote(p.Value); v == "off" || v == "never" {
				cfg.ResumeBelowPct = -1
				continue
			}
			n, err := kv.ParseInt(p.Value)
			if err != nil || n < 0 || n > 100 {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · resume_below_pct inválido: %s · entero de 1 a 100, o off para no reanudar"), p.Line, p.Value)
			}
			cfg.ResumeBelowPct = n
		case "estrategia":
			cfg.Estrategia = kv.Unquote(p.Value)
		case "reglas_import":
			list, err := kv.ParseList(p.Value)
			if err != nil {
				return cfg, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), p.Line, err)
			}
			cfg.ReglasImport = list
		}
	}

	proveedores, err := parseProveedores(data)
	if err != nil {
		return cfg, err
	}
	cfg.Proveedores = proveedores

	agentes, err := parseAgentes(data)
	if err != nil {
		return cfg, err
	}
	cfg.Agentes = agentes

	modelos, err := parseModelos(data)
	if err != nil {
		return cfg, err
	}
	cfg.Modelos = modelos

	ventanas, err := parsePresupuestoVentanas(data)
	if err != nil {
		return cfg, err
	}
	cfg.PresupuestoVentanas = ventanas
	return cfg, nil
}

// parsePresupuestoVentanas lee el bloque anidado `presupuesto:`, con un
// proveedor por línea: `claude: { 5h: 40000, semanal: 120000 }`.
func parsePresupuestoVentanas(data []byte) (map[string]map[string]int, error) {
	children, err := kv.Nested(strings.Split(string(data), "\n"), "presupuesto", 1)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("config.yml: %s"), err)
	}
	if len(children) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]int, len(children))
	for _, c := range children {
		fields, err := kv.ParseInlineMap(c.Value)
		if err != nil {
			return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), c.Line, err)
		}
		limites := make(map[string]int, len(fields))
		for ventana, raw := range fields {
			n, err := kv.ParseInt(raw)
			if err != nil || n < 0 {
				return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s %s inválido: %s · entero, mínimo 0"), c.Line, c.Key, ventana, raw)
			}
			if n > 0 {
				limites[ventana] = n
			}
		}
		out[c.Key] = limites
	}
	return out, nil
}

// parseModelos lee el bloque anidado `modelos:`, con un peso
// por línea: `liviana: glm-4`.
func parseModelos(data []byte) (map[string]string, error) {
	children, err := kv.Nested(strings.Split(string(data), "\n"), "modelos", 1)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("config.yml: %s"), err)
	}
	if len(children) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(children))
	for _, c := range children {
		out[c.Key] = kv.Unquote(c.Value)
	}
	return out, nil
}

// parseProveedores lee el bloque anidado `proveedores:`, con un
// rol por línea: `planificador: { modelo: X, key_env: Y }`.
func parseProveedores(data []byte) (map[string]Proveedor, error) {
	children, err := kv.Nested(strings.Split(string(data), "\n"), "proveedores", 1)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("config.yml: %s"), err)
	}
	if len(children) == 0 {
		return nil, nil
	}
	out := make(map[string]Proveedor, len(children))
	for _, c := range children {
		fields, err := kv.ParseInlineMap(c.Value)
		if err != nil {
			return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), c.Line, err)
		}
		out[c.Key] = Proveedor{Modelo: fields["modelo"], KeyEnv: fields["key_env"]}
	}
	return out, nil
}

// parseAgentes lee el bloque anidado `agentes:`, con un agente
// por línea: `architect: { provider: claude, model: claude-sonnet, skills: ["diseno"] }`.
func parseAgentes(data []byte) (map[string]Agente, error) {
	children, err := kv.Nested(strings.Split(string(data), "\n"), "agentes", 1)
	if err != nil {
		return nil, fmt.Errorf(idioma.T("config.yml: %s"), err)
	}
	if len(children) == 0 {
		return nil, nil
	}
	out := make(map[string]Agente, len(children))
	for _, c := range children {
		fields, err := kv.ParseInlineMap(c.Value)
		if err != nil {
			return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), c.Line, err)
		}
		provider := fields["provider"]
		if provider == "" {
			provider = fields["proveedor"]
		}
		if !slices.Contains(Clis, provider) {
			return nil, fmt.Errorf(idioma.T("config.yml: línea %d · provider desconocido: %s · usa %s"), c.Line, provider, strings.Join(Clis, ", "))
		}

		modelo := fields["model"]
		if modelo == "" {
			modelo = fields["modelo"]
		}

		agente := Agente{
			Provider: provider,
			Modelo:   modelo,
			KeyEnv:   fields["key_env"],
		}
		if rawSkills, ok := fields["skills"]; ok && rawSkills != "" {
			skills, err := kv.ParseList(rawSkills)
			if err != nil {
				return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), c.Line, err)
			}
			agente.Skills = skills
		}
		if rawPkgs, ok := fields["skill_packages"]; ok && rawPkgs != "" {
			pkgs, err := kv.ParseList(rawPkgs)
			if err != nil {
				return nil, fmt.Errorf(idioma.T("config.yml: línea %d · %s"), c.Line, err)
			}
			agente.SkillPackages = pkgs
		}
		out[c.Key] = agente
	}
	return out, nil
}

// Pesos son los tres niveles de `modelos:`, de menor a mayor gasto.
var Pesos = []string{"liviana", "media", "pesada"}

// pistas de tamaño en los ids de modelo, por peso. El orden importa:
// gana la primera que casa.
// Clis son los CLIs de agente que loom maneja: valen como `cli:`,
// como provider de un agente y como proveedor de ventanas de presupuesto.
// Tiene que coincidir con executor.Todos (config no puede importarlo).
var Clis = []string{"claude", "opencode", "codex"}

var pistasPeso = map[string][]string{
	"liviana": {"haiku", "flash", "lightning", "mini", "lite", "small", "free"},
	"pesada":  {"opus", "ultra", "-max", "-pro", "thinking"},
}

// preferidos son los modelos que loom elige primero, por peso y en
// orden, cuando el catálogo del CLI los tiene. Se comparan contra el id
// sin proveedor ("opencode-go/kimi-k3" casa con "kimi-k3"). Si ninguno
// está, se cae a las pistas por nombre.
var preferidos = map[string][]string{
	"pesada":  {"claude-opus-5-5", "gpt-6-astra", "kimi-k3", "grok-4.7", "grok-4.6"},
	"media":   {"claude-sonnet-5-5", "claude-sonnet-5", "gpt-6-sol", "gpt-5.6-terra", "deepseek-v4-flash", "deepseek-v4.1-flash"},
	"liviana": {"claude-haiku-5-5", "claude-haiku-4-5", "gpt-6-luna", "gpt-5.6-luna", "deepseek-flash", "muse-spark-1.3-contributor", "muse-spark-1.3-contributor-free", "muse-spark-1.2-contributor"},
}

// pasarelas son los proveedores de opencode que cobran por suscripción
// propia (Zen, Go): un modelo con la key del usuario (deepseek/…,
// openrouter/…) va antes, porque la suscripción puede estar vencida y su
// id sigue en el catálogo mientras quede la credencial vieja.
var pasarelas = []string{"opencode/", "opencode-go/"}

func dePasarela(m string) bool {
	for _, p := range pasarelas {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

// propiosPrimero ordena el catálogo con los modelos de key propia antes
// que los de pasarela, sin cambiar el orden dentro de cada grupo.
func propiosPrimero(catalogo []string) []string {
	var propios, ajenos []string
	for _, m := range catalogo {
		if dePasarela(m) {
			ajenos = append(ajenos, m)
		} else {
			propios = append(propios, m)
		}
	}
	return append(propios, ajenos...)
}

// ElegirModelos reparte un catálogo real de ids de modelo entre los tres
// pesos, para que `loom init` deje una configuración que funciona sin
// que el usuario adivine ids. Es una heurística por nombre y el usuario
// manda: `modelos:` en config.yml se edita a mano y gana siempre.
//
// ponytail: heurística por subcadena en el id; si un proveedor nombra sus
// modelos sin pistas de tamaño, todo cae en "media" y el usuario elige a
// mano. Cambiar a metadata real solo si algún CLI la expone.
func ElegirModelos(catalogo []string) map[string]string {
	if len(catalogo) == 0 {
		return nil
	}
	catalogo = propiosPrimero(catalogo)
	// un preferido de pasarela solo gana si ninguno de key propia está
	propios := 0
	for propios < len(catalogo) && !dePasarela(catalogo[propios]) {
		propios++
	}
	preferido := func(peso string) string {
		for _, grupo := range [][]string{catalogo[:propios], catalogo[propios:]} {
			for _, p := range preferidos[peso] {
				for _, m := range grupo {
					if m == p || strings.HasSuffix(m, "/"+p) {
						return m
					}
				}
			}
		}
		return ""
	}
	buscar := func(peso string) string {
		if m := preferido(peso); m != "" {
			return m
		}
		for _, pista := range pistasPeso[peso] {
			for _, m := range catalogo {
				if strings.Contains(strings.ToLower(m), pista) {
					return m
				}
			}
		}
		return ""
	}

	res := map[string]string{}
	liviana, pesada := buscar("liviana"), buscar("pesada")

	// media: el preferido o el primero que no sea ni el liviano ni el pesado
	media := preferido("media")
	for _, m := range catalogo {
		if media != "" {
			break
		}
		if m != liviana && m != pesada {
			media = m
			break
		}
	}
	if media == "" {
		media = catalogo[0]
	}
	if liviana == "" {
		liviana = media
	}
	if pesada == "" {
		pesada = media
	}
	res["liviana"], res["media"], res["pesada"] = liviana, media, pesada
	return res
}

// ModelosValidos separa los modelos configurados que el CLI no reconoce.
// `loom doctor` lo usa para avisar antes de una corrida, no después
// de quemarla.
func ModelosValidos(configurados, catalogo []string) (desconocidos []string) {
	if len(catalogo) == 0 {
		return nil
	}
	valido := make(map[string]bool, len(catalogo))
	for _, m := range catalogo {
		valido[m] = true
	}
	visto := map[string]bool{}
	for _, m := range configurados {
		if m == "" || valido[m] || visto[m] {
			continue
		}
		visto[m] = true
		desconocidos = append(desconocidos, m)
	}
	return desconocidos
}

// DefaultAgentes devuelve el catálogo de arquetipos predefinidos con modelos y skills listos para usar.
func DefaultAgentes(cli string) map[string]Agente {
	provider := cli
	if provider == "" {
		provider = "claude"
	}

	keyEnv := "ANTHROPIC_API_KEY"
	switch provider {
	case "opencode":
		keyEnv = "OPENCODE_API_KEY"
	case "codex":
		keyEnv = "OPENAI_API_KEY"
	}

	// Los arquetipos NO fijan modelo. Antes traían ids inventados
	// ("glm-5.2", "claude-sonnet") que ningún CLI acepta: cada
	// invocación moría en dos segundos, la tarea agotaba sus intentos y
	// la corrida entera se caía en cascada sin gastar un token. El
	// modelo sale de `modelos:` en config.yml (por peso de tarea), que
	// `loom init` rellena con el catálogo real del CLI; vacío
	// significa "usa el modelo por defecto del CLI", que siempre existe.
	const modeloDelCLI = ""

	return map[string]Agente{
		"ejecutor": {
			Provider: provider,
			Modelo:   modeloDelCLI,
			KeyEnv:   keyEnv,
			Skills:   []string{"implementacion", "tdd", "refactor"},
		},
		"backend": {
			Provider:      provider,
			Modelo:        modeloDelCLI,
			KeyEnv:        keyEnv,
			Skills:        []string{"backend", "api", "database", "sql", "performance"},
			SkillPackages: []string{skills.BackendSkillName()},
		},
		"frontend": {
			Provider:      provider,
			Modelo:        modeloDelCLI,
			KeyEnv:        keyEnv,
			Skills:        []string{"frontend", "ui", "ux", "components", "css", "state"},
			SkillPackages: []string{skills.UISkillName()},
		},
		"architect": {
			Provider: provider,
			Modelo:   modeloDelCLI,
			KeyEnv:   keyEnv,
			Skills:   []string{"arquitectura", "diseno", "contratos", "clean-code"},
		},
		"tester": {
			Provider:      provider,
			Modelo:        modeloDelCLI,
			KeyEnv:        keyEnv,
			Skills:        []string{"testing", "cobertura", "edge-cases", "examinador"},
			SkillPackages: []string{skills.PMSkillName()},
		},
		"refactor": {
			Provider: provider,
			Modelo:   modeloDelCLI,
			KeyEnv:   keyEnv,
			Skills:   []string{"refactoring", "simplificacion", "deuda-tecnica"},
		},
	}
}

// ObtenerAgente resuelve un agente por nombre buscando primero en la configuración
// personalizada del usuario (c.Agentes), luego en los arquetipos predefinidos (DefaultAgentes)
// y finalmente en c.Proveedores.
func (c Config) ObtenerAgente(nombre string) (Agente, bool) {
	if nombre == "" {
		nombre = "ejecutor"
	}

	// 1. Configuración explícita del usuario
	if ag, ok := c.Agentes[nombre]; ok {
		if ag.Provider == "" {
			ag.Provider = c.Cli
		}
		if ag.Provider == "" {
			ag.Provider = "claude"
		}
		return ag, true
	}

	// 2. Arquetipos predefinidos estándar
	defaults := DefaultAgentes(c.Cli)
	if ag, ok := defaults[nombre]; ok {
		return ag, true
	}

	// 3. Bloque proveedores clásico
	if p, ok := c.Proveedores[nombre]; ok {
		provider := c.Cli
		if provider == "" {
			provider = "claude"
		}
		return Agente{
			Provider: provider,
			Modelo:   p.Modelo,
			KeyEnv:   p.KeyEnv,
		}, true
	}

	return Agente{}, false
}

// TodosLosAgentes devuelve el mapa completo de agentes disponibles, fusionando
// los arquetipos por defecto con las personalizaciones del usuario.
func (c Config) TodosLosAgentes() map[string]Agente {
	res := DefaultAgentes(c.Cli)
	for k, v := range c.Agentes {
		res[k] = v
	}
	return res
}

// DefaultResumeBelowPct is the context % under which attempts resume the claude session.
const DefaultResumeBelowPct = 70

// listaFlexible reads ["a", "b"] or [a, b]: dna names are plain words.
func listaFlexible(v string) []string {
	var out []string
	for _, x := range strings.Split(strings.Trim(strings.TrimSpace(v), "[]"), ",") {
		if x = strings.Trim(strings.TrimSpace(x), `"'`); x != "" {
			out = append(out, x)
		}
	}
	return out
}

// ResumeBelow is the effective resume threshold: the default when unset,
// 0 (never) when switched off.
func (c Config) ResumeBelow() int {
	switch {
	case c.ResumeBelowPct < 0:
		return 0
	case c.ResumeBelowPct == 0:
		return DefaultResumeBelowPct
	}
	return c.ResumeBelowPct
}
