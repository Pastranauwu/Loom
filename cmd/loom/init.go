package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/task"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/gate"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/tui"
)

type initResult struct {
	Base    string `json:"base"`
	Pruebas string `json:"pruebas"`
	Config  string `json:"config"`
}

func newInitCmd() *cobra.Command {
	var pruebas, plantilla, cli string
	var sinSkills bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "detecta el repo y crea .loom/",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			var in io.Reader
			if pruebas == "" && plantilla == "" && !out.JSON() && isTerminal(os.Stdin) {
				in = os.Stdin
			}
			return runInit(cwd, pruebas, plantilla, cli, in, sinSkills)
		},
	}
	cmd.Flags().StringVar(&cli, "cli", "", "CLI de agente: opencode, claude o codex (por defecto pregunta si hay más de uno)")
	cmd.Flags().StringVar(&pruebas, "tests", "", "comando de pruebas del proyecto, en vez del detectado")
	cmd.Flags().StringVar(&plantilla, "tests-template", "", "stack de pruebas: go, node o python")
	cmd.Flags().BoolVar(&sinSkills, "no-skills", false, "no traer las skills por defecto (puedes hacerlo luego con loom skills sync)")
	return cmd
}

// confirmarPruebas shows the detected test command and takes a
// correction: una detección silenciosa equivocada cuesta
// horas. Enter en blanco acepta lo detectado.
func confirmarPruebas(in io.Reader, detectado string) string {
	if detectado == "" {
		out.Line(idioma.T("· comando de pruebas no detectado · escribe el de este proyecto, o enter para dejarlo vacío"))
	} else {
		out.Line(idioma.T("· comando de pruebas detectado: %s"), detectado)
		out.Line(idioma.T("  enter para aceptarlo, o escribe el correcto"))
	}
	linea, _ := bufio.NewReader(in).ReadString('\n')
	if respuesta := strings.TrimSpace(linea); respuesta != "" {
		return respuesta
	}
	return detectado
}

func runInit(cwd, pruebasFlag, plantilla, cli string, in io.Reader, sinSkills bool) error {
	root, err := config.RepoRoot(cwd)
	if err != nil {
		return err
	}
	if config.Exists(root) {
		return errors.New(idioma.T("ya existe .loom en este repo · nada que hacer"))
	}

	if in != nil && esTUI() && os.Getenv("LOOM_LANGUAGE") == "" {
		if l, err := elegirIdioma(); err == nil && l != "" {
			idioma.Set(l)
		}
	}

	base := config.DetectBaseBranch(root)
	pruebas, pruebasOK := config.DetectTestCommand(root)
	switch {
	case pruebasFlag != "":
		pruebas, pruebasOK = pruebasFlag, true
	case plantilla != "":
		cmd, ok := config.PlantillaPruebas(plantilla)
		if !ok {
			return errors.New(idioma.T("plantilla desconocida: ") + plantilla + " · usa go, node o python")
		}
		pruebas, pruebasOK = cmd, true
	case in != nil:
		pruebas = confirmarPruebas(in, pruebas)
		pruebasOK = pruebas != ""
	}

	if err := os.MkdirAll(config.TasksDir(root), 0o755); err != nil {
		return err
	}
	// lo que se versiona es el plan (tasks, config, feature, intención);
	// cuartos, logs de corridas y capturas son de la máquina
	gitignore := filepath.Join(config.Dir(root), ".gitignore")
	if err := os.WriteFile(gitignore, []byte("rooms/\nruns/\ncorridas/\npuntas.json\n"), 0o644); err != nil {
		return err
	}
	cfg := config.Config{
		Base:            base,
		Language:        idioma.Actual(),
		Pruebas:         pruebas,
		ZonasProhibidas: config.DefaultForbiddenZones(),
		PatronesPrueba:  config.DefaultTestPatterns(),
		TimeoutEsclusa:  int(gate.DefaultTimeout / time.Second),
		TimeoutAgente:   int(loop.DefaultAgentTimeout / time.Second),
		TimeoutPruebas:  int(loop.DefaultTimeout / time.Second),
	}
	// El CLI y sus modelos se descubren aquí, una sola vez, y quedan
	// escritos en config.yml: es la única forma de que los ids sean
	// reales y de que el usuario los pueda cambiar sin leer el código.
	// Con más de un CLI instalado, en terminal elige el humano: antes
	// se tomaba el primero (opencode) y el catálogo de claude nunca se veía.
	if cli == "" && in != nil && esTUI() {
		var err error
		cli, err = elegirCLIAMano(clisInstalados())
		if err != nil {
			return err
		}
	}
	cliDetectado, catalogo := detectarCatalogo(cli)
	modelos := config.ElegirModelos(catalogo)
	// la heurística acierta el tamaño por el nombre, no la calidad ni la
	// velocidad: en terminal el humano ve el catálogo real y decide
	if in != nil && esTUI() && len(catalogo) > 0 {
		if m, err := elegirModelosAMano(modelos, catalogo); err != nil {
			return err
		} else if m != nil {
			modelos = m
		}
	}
	cfg.Cli, cfg.Modelos = cliDetectado, modelos
	if err := cfg.Save(root); err != nil {
		return err
	}

	if err := out.Data(initResult{Base: base, Pruebas: pruebas, Config: config.Path(root)}); err != nil {
		return err
	}
	out.Line(idioma.T("✓ repositorio detectado"))
	if base != "" {
		out.Line(idioma.T("✓ rama base: %s"), base)
	} else {
		out.Line(idioma.T("· rama base no detectada · edita .loom/config.yml"))
	}
	if pruebasOK {
		out.Line(idioma.T("✓ comando de pruebas detectado: %s"), pruebas)
	} else {
		out.Line(idioma.T("· comando de pruebas no detectado · edita .loom/config.yml"))
	}
	if config.DetectEmpty(root) {
		out.Line(idioma.T("· repositorio vacío · loom plan arrancará desde cero proponiendo un stack"))
	}
	if cliDetectado != "" {
		out.Line(idioma.T("✓ ejecutor: %s"), cliDetectado)
	} else {
		out.Line(idioma.T("· ningún ejecutor instalado · instala opencode o claude"))
	}
	if len(modelos) > 0 {
		for _, peso := range config.Pesos {
			out.Line(idioma.T("✓ modelo %s: %s"), peso, modelos[peso])
		}
	} else if cliDetectado != "" {
		out.Line(idioma.T("· sin catálogo de modelos · se usará el modelo por defecto de %s"), cliDetectado)
	}
	out.Line(idioma.T("✓ configuración creada en .loom/"))
	out.Line(idioma.T("· nuevo en loom · corre loom tutorial para un recorrido guiado"))
	if sinSkills {
		out.Line(idioma.T("· skills sin traer · corre loom skills sync cuando quieras"))
	} else {
		out.Line(idioma.T("· trayendo skills por defecto (npx skills add) …"))
		if err := runSkillsSync(); err != nil {
			out.Line(idioma.T("· no se pudieron traer las skills · %s · reintenta con loom skills sync"), err)
		}
	}
	stack := config.StackDePlantilla(plantilla)
	if stack == "" {
		stack = config.DetectLanguage(root)
	}
	imprimirPlantillasListoCuando(stack)
	return nil
}

// detectarCatalogo elige el ejecutor pedido (o el primero instalado) y
// le pide su catálogo real de modelos. Sin ejecutor, o si el CLI no sabe
// listarlos, devuelve vacío: `modelos:` queda fuera de config.yml y cada
// invocación usa el modelo por defecto del CLI, que siempre existe.
func detectarCatalogo(cli string) (string, []string) {
	ex, err := elegirEjecutor(cli)
	if err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalogo, err := ex.Models(ctx)
	if err != nil {
		return ex.Name(), nil
	}
	return ex.Name(), catalogo
}

// clisInstalados devuelve los nombres de los CLIs de agente que responden.
func clisInstalados() []string {
	var res []string
	for _, e := range executor.Todos {
		if e.Available() == nil {
			res = append(res, e.Name())
		}
	}
	return res
}

// elegirCLIAMano pregunta qué CLI usar cuando hay más de uno. Con uno
// solo (o ninguno) no pregunta y devuelve "" para que decida la
// autodetección.
func elegirCLIAMano(instalados []string) (string, error) {
	if len(instalados) < 2 {
		return "", nil
	}
	ops := make([]tui.Opcion, 0, len(instalados))
	for _, n := range instalados {
		detalle := idioma.T("Usa los proveedores y modelos configurados en OpenCode")
		switch n {
		case "claude":
			detalle = idioma.T("Usa tu sesión de Claude · Opus planifica, Haiku y Sonnet implementan")
		case "codex":
			detalle = idioma.T("Usa tu sesión de Codex (ChatGPT) y sus modelos gpt")
		}
		ops = append(ops, tui.Opcion{ID: n, Etiqueta: n, Detalle: detalle})
	}
	id, err := tui.Elegir(idioma.T("CLI DE AGENTE"), idioma.T("↑/↓ mueve · enter elige · esc o ctrl+c cancela"), ops)
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New(idioma.T("inicio cancelado · vuelve a ejecutar loom up cuando quieras"))
	}
	return id, nil
}

// elegirModelosAMano deja al humano fijar el modelo de cada peso sobre el
// catálogo real del CLI. Devuelve nil si prefiere quedarse con la
// propuesta automática.
func elegirModelosAMano(propuesta map[string]string, catalogo []string) (map[string]string, error) {
	var resumen []string
	for _, peso := range config.Pesos {
		resumen = append(resumen, peso+": "+propuesta[peso])
	}
	quiere, err := tui.Elegir(idioma.T("MODELOS POR PESO DE TAREA"),
		idioma.T("j/k mueve · enter elige · q deja la propuesta"),
		[]tui.Opcion{
			{ID: "auto", Etiqueta: idioma.T("usar la propuesta"), Detalle: strings.Join(resumen, " · ")},
			{ID: "mano", Etiqueta: idioma.T("elegir yo"), Detalle: fmt.Sprintf(idioma.T("%d modelos disponibles en el CLI"), len(catalogo))},
		})
	if err != nil || quiere != "mano" {
		return nil, err
	}
	return elegirPorPeso(propuesta, catalogo)
}

// elegirPorPeso es el panel de los tres pesos: una lista del catálogo
// real del CLI por peso. Cancelar un peso deja el de la propuesta.
func elegirPorPeso(propuesta map[string]string, catalogo []string) (map[string]string, error) {
	ops := make([]tui.Opcion, 0, len(catalogo))
	for _, m := range catalogo {
		ops = append(ops, tui.Opcion{ID: m, Etiqueta: m})
	}
	elegidos := map[string]string{}
	for _, peso := range config.Pesos {
		id, err := tui.Elegir(idioma.T("MODELO PARA TAREAS DE PESO ")+strings.ToUpper(pesoEn(peso)),
			idioma.T("j/k mueve · enter elige · q deja ")+propuesta[peso], ops)
		if err != nil {
			return nil, err
		}
		if id == "" {
			id = propuesta[peso] // canceló: se queda el propuesto
		}
		elegidos[peso] = id
	}
	return elegidos, nil
}

// pesoEn shows a weight in the language of the interface: light, medium, heavy.
func pesoEn(peso string) string {
	if idioma.Es() {
		return peso
	}
	return task.PesoEn(peso)
}
