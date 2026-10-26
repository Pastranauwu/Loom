package main

import (
	"context"
	"errors"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/executor"
	"github.com/Pastranauwu/loom/internal/overlap"
	"github.com/Pastranauwu/loom/internal/ship"
)

type chequeoDoctor struct {
	Nombre  string `json:"nombre"`
	OK      bool   `json:"ok"`
	Detalle string `json:"detalle,omitempty"`
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "verifica git, configuración, keys y ejecutores",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

// runDoctor verifica el entorno: git, repo, configuración, keys y
// al menos un ejecutor instalado. Reporta todo y falla solo si algo
// crítico falta.
func runDoctor() error {
	var checks []chequeoDoctor

	// git
	if _, err := exec.LookPath("git"); err != nil {
		checks = append(checks, chequeoDoctor{"git", false, "git no está instalado"})
	} else if !overlap.SoportaWriteTree() {
		// sin --write-tree la deteccion textual de solapamiento no
		// funciona, y antes se apagaba en silencio: el usuario creia
		// tener el chequeo y no lo tenia
		checks = append(checks, chequeoDoctor{"git", false,
			"git " + overlap.VersionGit() + " · sin 'merge-tree --write-tree' (necesita 2.38+) · la detección de solapamiento entre tareas queda apagada"})
	} else {
		checks = append(checks, chequeoDoctor{"git", true, ""})
	}

	// repo
	root, repoErr := config.RepoRoot(mustGetwd())
	if repoErr != nil {
		checks = append(checks, chequeoDoctor{"repositorio", false, repoErr.Error()})
	} else {
		checks = append(checks, chequeoDoctor{"repositorio", true, root})
	}

	// configuración
	var cfg config.Config
	if repoErr == nil && config.Exists(root) {
		if c, err := config.Load(root); err != nil {
			checks = append(checks, chequeoDoctor{"configuración", false, err.Error()})
		} else {
			cfg = c
			checks = append(checks, chequeoDoctor{"configuración", true, ""})
		}
	} else {
		checks = append(checks, chequeoDoctor{"configuración", false, "sin .loom · corre loom init"})
	}

	// ejecutores
	ejecutores := executor.Todos
	var disponibles []string
	for _, e := range ejecutores {
		if err := e.Available(); err == nil {
			disponibles = append(disponibles, e.Name())
		}
	}
	if len(disponibles) > 0 {
		checks = append(checks, chequeoDoctor{"ejecutores", true, strings.Join(disponibles, ", ")})
	} else {
		checks = append(checks, chequeoDoctor{"ejecutores", false, "ninguno instalado · instala opencode o claude"})
	}

	// modelos: los ids configurados deben existir en el catálogo del CLI.
	// Es el chequeo que faltaba: un id inventado no falla en doctor, falla
	// en mitad de la corrida, después de crear cuartos y quemar intentos.
	if len(disponibles) > 0 {
		checks = append(checks, chequearModelos(cfg, disponibles))
	}

	// keys: al menos una variable de proveedor en el entorno
	keys := []string{"OPENCODE_API_KEY", "ANTHROPIC_API_KEY", "DEEPSEEK_API_KEY", "OPENAI_API_KEY"}
	for _, p := range cfg.Proveedores {
		if p.KeyEnv != "" {
			keys = append(keys, p.KeyEnv)
		}
	}
	for _, a := range cfg.Agentes {
		if a.KeyEnv != "" {
			keys = append(keys, a.KeyEnv)
		}
	}
	var presentes []string
	for _, k := range keys {
		if os.Getenv(k) != "" {
			presentes = append(presentes, k)
		}
	}
	if len(presentes) > 0 {
		checks = append(checks, chequeoDoctor{"keys", true, strings.Join(presentes, ", ")})
	} else {
		checks = append(checks, chequeoDoctor{"keys", false, "ninguna key de proveedor en el entorno"})
	}

	// remoto: con varias cuentas de gh, la activa puede no entrar al
	// origin; sin este chequeo se descubre al final de la entrega
	if repoErr == nil && cfg.Entrega != config.EntregaLocal {
		if motivo := ship.AccesoRemoto(root); motivo != "" {
			checks = append(checks, chequeoDoctor{"remoto", false, motivo})
		}
	}

	// npx: opcional, solo hace falta para loom skills sync
	if _, err := exec.LookPath("npx"); err != nil {
		checks = append(checks, chequeoDoctor{"npx", false, "no instalado · loom skills sync no va a poder traer skills"})
	} else {
		checks = append(checks, chequeoDoctor{"npx", true, ""})
	}

	if err := out.Data(checks); err != nil {
		return err
	}
	critico := false
	for _, c := range checks {
		if c.OK {
			out.Line("✓ %s%s", idioma.T(c.Nombre), detalleDoctor(c))
		} else {
			out.Line("✗ %s  · %s", idioma.T(c.Nombre), idioma.T(c.Detalle))
			if c.Nombre != "keys" && c.Nombre != "npx" && c.Nombre != "remoto" {
				critico = true
			}
		}
	}
	if critico {
		return errors.New(idioma.T("el entorno no está listo · arregla lo marcado con ✗"))
	}
	out.Line(idioma.T("entorno listo"))
	_ = cfg
	return nil
}

func detalleDoctor(c chequeoDoctor) string {
	if c.Detalle == "" {
		return ""
	}
	return "  · " + idioma.T(c.Detalle)
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// chequearModelos contrasta cada modelo declarado en config.yml contra
// el catálogo real del CLI por defecto.
func chequearModelos(cfg config.Config, disponibles []string) chequeoDoctor {
	nombre := cfg.Cli
	if nombre == "" {
		nombre = disponibles[0]
	}
	ex, err := elegirEjecutor(nombre)
	if err != nil {
		return chequeoDoctor{"modelos", false, err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	catalogo, err := ex.Models(ctx)
	if err != nil {
		return chequeoDoctor{"modelos", true, idioma.T("catálogo no consultable · se usará el modelo por defecto de ") + ex.Name()}
	}

	var configurados []string
	for _, peso := range config.Pesos {
		configurados = append(configurados, cfg.Modelos[peso])
	}
	for _, a := range cfg.Agentes {
		configurados = append(configurados, a.Modelo)
	}
	for _, p := range cfg.Proveedores {
		configurados = append(configurados, p.Modelo)
	}

	if malos := config.ModelosValidos(configurados, catalogo); len(malos) > 0 {
		return chequeoDoctor{"modelos", false,
			"no existen en " + ex.Name() + ": " + strings.Join(malos, ", ") +
				" · corrígelos en .loom/config.yml (ver `" + ex.Name() + " models`)"}
	}
	var declarados []string
	for _, peso := range config.Pesos {
		if m := cfg.Modelos[peso]; m != "" {
			declarados = append(declarados, peso+"="+m)
		}
	}
	if len(declarados) == 0 {
		return chequeoDoctor{"modelos", true, idioma.T("sin `modelos:` · se usará el modelo por defecto de ") + ex.Name()}
	}
	return chequeoDoctor{"modelos", true, strings.Join(declarados, ", ")}
}
