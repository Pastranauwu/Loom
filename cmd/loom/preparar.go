package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/room"
)

// entornoListo es prepararEntorno desde el cwd y la terminal, una sola
// vez por proceso: up encadena plan y run, que también lo llaman, y el
// catálogo de modelos no hace falta consultarlo tres veces.
var entornoPreparado *struct {
	root string
	cfg  config.Config
}

func entornoListo(entregar bool) (string, config.Config, error) {
	return entornoListoConCLI(entregar, "")
}

func entornoListoConCLI(entregar bool, cli string) (string, config.Config, error) {
	if entornoPreparado != nil {
		return entornoPreparado.root, entornoPreparado.cfg, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", config.Config{}, err
	}
	// el orquestador configura; el humano solo pide. Lo que no se
	// pueda arreglar solo se pregunta en terminal.
	var in io.Reader
	if !out.JSON() && isTerminal(os.Stdin) {
		in = os.Stdin
	}
	root, cfg, err := prepararEntornoConCLI(cwd, in, entregar, cli)
	if err != nil {
		return "", config.Config{}, err
	}
	entornoPreparado = &struct {
		root string
		cfg  config.Config
	}{root, cfg}
	return root, cfg, nil
}

// prepararEntorno deja el repo listo para `up` sin que el humano
// configure nada a mano: git init, .loom, commit inicial, ejecutor,
// modelos, comando de pruebas y, si va a entregar, gh y el remoto. Lo
// que se puede arreglar solo se arregla y se avisa en una línea; lo que
// no, se pregunta en terminal (`in` no nil) o se corta con qué hacer.
// Devuelve la raíz del repo y la configuración ya saneada.
func prepararEntorno(cwd string, in io.Reader, entregar bool) (string, config.Config, error) {
	return prepararEntornoConCLI(cwd, in, entregar, "")
}

func prepararEntornoConCLI(cwd string, in io.Reader, entregar bool, cliPreferido string) (string, config.Config, error) {
	var lector *bufio.Reader
	if in != nil {
		lector = bufio.NewReader(in)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return "", config.Config{}, errors.New(idioma.T("git no está instalado · instálalo y vuelve a correr"))
	}

	// 1. repositorio
	root, err := config.RepoRoot(cwd)
	if err != nil {
		if _, err := gitEn(cwd, "init", "--quiet"); err != nil {
			return "", config.Config{}, fmt.Errorf(idioma.T("no se pudo crear el repo git · %s"), err)
		}
		out.Line(idioma.T("· sin repo git · creado con git init"))
		if root, err = config.RepoRoot(cwd); err != nil {
			return "", config.Config{}, err
		}
	}

	// 2. .loom
	if !config.Exists(root) {
		mostrarInicioProyecto(root)
		// con dos CLIs instalados, la única decisión que vale preguntar
		cli := cliPreferido
		if cli == "" && lector != nil && esTUI() {
			var err error
			cli, err = elegirCLIAMano(clisInstalados())
			if err != nil {
				return "", config.Config{}, err
			}
		}
		if err := runInit(root, "", "", cli, nil, false); err != nil {
			return "", config.Config{}, err
		}
	}
	cfg, err := config.Load(root)
	if err != nil {
		return "", config.Config{}, err
	}
	guardar := false

	// 3. rama base: vacía o apuntando a una rama que no existe. Un
	// `base: master` heredado en un repo cuya rama es `main` detenía
	// las cuatro tareas de la corrida sin arreglo posible desde el CLI.
	if cfg.Base == "" || (tieneCommits(root) && !ramaExiste(root, cfg.Base)) {
		anterior := cfg.Base
		if cfg.Base = config.DetectBaseBranch(root); cfg.Base == "" {
			cfg.Base = "main"
		}
		if anterior != "" && anterior != cfg.Base {
			out.Line(idioma.T("· la rama base %s no existe · se usa %s"), anterior, cfg.Base)
		} else {
			out.Line(idioma.T("· rama base: %s"), cfg.Base)
		}
		guardar = true
	}
	// los cuartos salen de la base y la entrega va a ella: en otra rama,
	// lo que hay aquí no lo ven los agentes
	if actual, err := gitEn(root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		if actual = strings.TrimSpace(actual); actual != "" && actual != cfg.Base && !strings.HasPrefix(actual, "loom/") && !strings.HasPrefix(actual, "devclean/") {
			out.Line(idioma.T("· estás en %s pero la base es %s · los agentes parten de %s y ahí se entrega · cambia `base:` en .loom/config.yml si no es lo que quieres"), actual, cfg.Base, cfg.Base)
		}
	}

	// 4. commit inicial: los cuartos son worktrees y sin commit no nacen
	if _, err := gitEn(root, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		if err := commitInicial(root, lector); err != nil {
			return "", config.Config{}, err
		}
	}

	// 5. Una selección explícita debe aplicarse también a la planificación.
	if cliPreferido != "" && cfg.Cli != cliPreferido {
		cfg.Cli, guardar = cliPreferido, true
		cfg.Modelos = nil // se descubrirá el catálogo del CLI seleccionado
	}
	ex, err := elegirEjecutor(cfg.Cli)
	if err != nil && cfg.Cli != "" && cliPreferido == "" {
		if ex, err = elegirEjecutor(""); err == nil {
			out.Line(idioma.T("· %s no está instalado · se usa %s"), cfg.Cli, ex.Name())
		}
	}
	if err != nil && cliPreferido != "" {
		return "", config.Config{}, err
	}
	if err != nil {
		ex, err = instalarEjecutor(lector)
		if err != nil {
			return "", config.Config{}, err
		}
	}
	if cfg.Cli != ex.Name() {
		cfg.Cli, guardar = ex.Name(), true
	}
	// 6. modelos: ids que el CLI no reconoce mueren en cada intento
	if catalogo, err := catalogoDe(ex); err == nil && len(catalogo) > 0 {
		if cambiados := revisarModelos(&cfg, ex, catalogo); len(cambiados) > 0 {
			guardar = true
			for _, c := range cambiados {
				out.Line("· %s", c)
			}
		}
	}

	// 7. comando de pruebas: sin él, ship no verifica el conjunto
	if strings.TrimSpace(cfg.Pruebas) == "" && !config.DetectEmpty(root) {
		if p, ok := config.DetectTestCommand(root); ok {
			cfg.Pruebas, guardar = p, true
			out.Line(idioma.T("· comando de pruebas detectado: %s"), p)
		} else if lector != nil {
			if p := confirmarPruebas(lector, ""); p != "" {
				cfg.Pruebas, guardar = p, true
			}
		}
		if strings.TrimSpace(cfg.Pruebas) == "" {
			out.Line(idioma.T("· sin comando de pruebas · ship no va a verificar el conjunto · decláralo en .loom/config.yml"))
		}
	}

	if guardar {
		if err := cfg.Save(root); err != nil {
			return "", config.Config{}, err
		}
	}

	// 8. entrega: fallar acá, antes de gastar un token, no al final
	if entregar {
		if err := prepararEntrega(root, cfg); err != nil {
			return "", config.Config{}, err
		}
	}
	return root, cfg, nil
}

// commitInicial crea el primer commit. Si lo único sin versionar es lo
// que loom acaba de escribir (.loom, las skills), lo hace solo;
// si hay más archivos, pregunta antes de meter al historial algo que el
// humano no revisó.
func commitInicial(root string, lector *bufio.Reader) error {
	estado, _ := gitEn(root, "status", "--porcelain")
	soloNuestro := true
	for _, l := range strings.Split(strings.TrimSpace(estado), "\n") {
		if l == "" {
			continue
		}
		ruta := strings.TrimSpace(l[3:])
		if !strings.HasPrefix(ruta, config.DirName) && !strings.HasPrefix(ruta, ".agents") && ruta != "skills-lock.json" {
			soloNuestro = false
			break
		}
	}
	if !soloNuestro {
		if lector == nil {
			return errors.New(idioma.T("no hay commits y hay archivos sin versionar · haz un commit inicial y reintenta"))
		}
		if !aprobar(lector, "no hay commits · ¿hago un commit inicial con todo lo que hay? [s/N]") {
			return errors.New(idioma.T("sin commit inicial · hazlo vos y vuelve a correr"))
		}
	}
	// sin .gitignore, el commit inicial se llevaría node_modules y .venv
	_ = room.ExcluirArtefactos(context.Background(), root)
	if _, err := gitEn(root, "add", "-A"); err != nil {
		return fmt.Errorf(idioma.T("no se pudo preparar el commit inicial · %s"), err)
	}
	if _, err := gitEn(root, "commit", "--quiet", "--allow-empty", "-m", "chore: inicio"); err != nil {
		return fmt.Errorf(idioma.T("no se pudo hacer el commit inicial · %s"), err)
	}
	out.Line(idioma.T("· sin commits · commit inicial creado"))
	return nil
}

// instalarEjecutor ofrece instalar un CLI de agente con npm cuando no
// hay ninguno. Solo en terminal y solo con permiso: instalar globales
// sin preguntar no es plug and play, es intrusión.
func instalarEjecutor(lector *bufio.Reader) (executor.Executor, error) {
	faltan := errors.New(idioma.T("ningún ejecutor instalado · instala opencode (npm i -g opencode-ai) o claude (npm i -g @anthropic-ai/claude-code) y vuelve a correr"))
	if lector == nil {
		return nil, faltan
	}
	if _, err := exec.LookPath("npm"); err != nil {
		return nil, faltan
	}
	if !aprobar(lector, "ningún ejecutor instalado · ¿instalo claude con npm i -g @anthropic-ai/claude-code? [s/N]") {
		return nil, faltan
	}
	cmd := exec.Command("npm", "i", "-g", "@anthropic-ai/claude-code")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf(idioma.T("npm no pudo instalar claude · %s"), err)
	}
	ex, err := elegirEjecutor("claude")
	if err != nil {
		return nil, err
	}
	out.Line(idioma.T("✓ claude instalado · inicia sesión con `claude` si todavía no lo hiciste"))
	return ex, nil
}

// prepararEntrega verifica lo que `ship` necesita. Sin remoto origin el PR
// es local (rama + descripción en .loom/pr/) y no hace falta nada más;
// con remoto, gh abre el PR y tiene que estar instalado.
func prepararEntrega(root string, cfg config.Config) error {
	if cfg.Entrega == config.EntregaLocal {
		return nil
	}
	if _, err := gitEn(root, "remote", "get-url", "origin"); err != nil {
		out.Line(idioma.T("· sin remoto origin · el PR queda local: la rama y su descripción en .loom/pr/"))
		return nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New(idioma.T("--ship necesita gh para abrir el PR en origin · instálalo (https://cli.github.com) o corre sin --ship"))
	}
	return nil
}

// revisarModelos reemplaza solo los modelos de `modelos:` que su CLI no
// reconoce, cada uno contra el catálogo del CLI que de verdad lo corre
// (ejecutorPara): con modelos mezclados, un id de opencode no está en el
// catálogo de claude y no por eso es inválido. Antes se validaba todo
// contra el CLI de `cli:` y un solo modelo desconocido reasignaba los
// tres: una config con sonnet como pesada amanecía con opus.
func revisarModelos(cfg *config.Config, ex executor.Executor, catalogo []string) []string {
	elegidos := config.ElegirModelos(catalogo)
	if len(cfg.Modelos) == 0 {
		cfg.Modelos = elegidos
		var out []string
		for _, peso := range config.Pesos {
			out = append(out, fmt.Sprintf("modelo %s: %s", peso, elegidos[peso]))
		}
		return out
	}
	catalogos := map[string][]string{ex.Name(): catalogo}
	var out []string
	for _, peso := range config.Pesos {
		m := cfg.Modelos[peso]
		if m == "" {
			continue
		}
		exM := ejecutorPara(ex, m)
		cat, ok := catalogos[exM.Name()]
		if !ok {
			cat, _ = catalogoDe(exM)
			catalogos[exM.Name()] = cat
		}
		// sin catálogo no hay con qué decir que es inválido
		if len(cat) == 0 || len(config.ModelosValidos([]string{m}, cat)) == 0 {
			continue
		}
		cfg.Modelos[peso] = elegidos[peso]
		out = append(out, fmt.Sprintf("modelo %s: %s no existe en %s · se usa %s", peso, m, exM.Name(), elegidos[peso]))
	}
	return out
}

func catalogoDe(ex executor.Executor) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return ex.Models(ctx)
}

func preguntar(lector *bufio.Reader, msg string) string {
	out.Line("%s", msg)
	l, _ := lector.ReadString('\n')
	return strings.TrimSpace(l)
}

func aprobar(lector *bufio.Reader, msg string) bool {
	r := strings.ToLower(preguntar(lector, msg))
	return r == "s" || r == "si" || r == "sí" || r == "y"
}

func tieneCommits(root string) bool {
	_, err := gitEn(root, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

func ramaExiste(root, rama string) bool {
	_, err := gitEn(root, "rev-parse", "--verify", "--quiet", rama+"^{commit}")
	return err == nil
}

func gitEn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	salida, err := cmd.CombinedOutput()
	return string(salida), err
}
