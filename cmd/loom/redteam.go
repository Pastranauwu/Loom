package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/dna"
	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/redteam"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/spec"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// redteamFlag is --redteam on ship: warn or block, overriding config.yml.
var redteamFlag string

// maxTareasRedteam bounds the cost of --all: the tasks that expose the most.
const maxTareasRedteam = 5

func modoRedteam(cfg config.Config) string {
	if redteamFlag != "" {
		return redteamFlag
	}
	if cfg.Redteam == "warn" || cfg.Redteam == "block" {
		return cfg.Redteam
	}
	return "off"
}

func newRedteamCmd() *cobra.Command {
	var todas, conservar, reabrir, dry bool
	cmd := &cobra.Command{
		Use:   "redteam [T-00N]",
		Short: "a saboteur tries to break what a task exposes before it ships",
		Long: `One model call writes hostile tests against the signatures a task exposes, and
code decides what they prove. Only a crash or race inside the task's own code
that reproduces twice is confirmed; an assertion the attacker invented is a
claim, and a test that does not compile is thrown away. The attack runs in a
throwaway worktree that is always removed.

--reopen turns each confirmed crash into a failing test inside the task that
owns the signature and reopens it, so loom run patches it. Set redteam: warn or
block in config.yml (or loom ship --redteam) to run it in the exit gate.`,
		Example: "  loom redteam T-003\n  loom redteam --all --reopen\n  loom redteam T-003 --keep-tests",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRedteam(args, todas, conservar, reabrir, dry)
		},
	}
	cmd.Flags().BoolVar(&todas, "all", false, "attack every ready task that exposes something (at most 5)")
	cmd.Flags().BoolVar(&conservar, "keep-tests", false, "keep the failing hostile tests in .loom/redteam/")
	cmd.Flags().BoolVar(&reabrir, "reopen", false, "reopen the owner of each confirmed crash with the reproducing test")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "list the tasks that would be attacked without spending a model call")
	return cmd
}

func runRedteam(args []string, todas, conservar, reabrir, dry bool) error {
	root, cfg, err := entornoListo(false)
	if err != nil {
		return err
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return err
	}
	var objetivo []task.Task
	for _, t := range tareas {
		st, _ := state.Get(root, t.ID)
		switch {
		case len(args) == 1 && t.ID == args[0]:
			objetivo = append(objetivo, t)
		case len(args) == 0 && todas && st.Estado == state.Lista && len(t.Expone) > 0:
			objetivo = append(objetivo, t)
		}
	}
	if len(args) == 0 && !todas {
		return errors.New(idioma.T("di qué atacar · loom redteam T-001 o loom redteam --all"))
	}
	if len(objetivo) == 0 {
		return errors.New(idioma.T("ninguna tarea lista expone algo que atacar"))
	}
	sort.SliceStable(objetivo, func(i, j int) bool { return len(objetivo[i].Expone) > len(objetivo[j].Expone) })
	if len(objetivo) > maxTareasRedteam {
		objetivo = objetivo[:maxTareasRedteam]
	}
	if dry {
		for _, t := range objetivo {
			out.Line(idioma.T("· se atacaría %s · %s · %d firmas expuestas · 1 llamada al modelo"), t.ID, t.Titulo, len(t.Expone))
		}
		return nil
	}
	gen, err := generadorRedteam(root, cfg)
	if err != nil {
		return err
	}
	informes := map[string]redteam.Informe{}
	for _, t := range objetivo {
		var inf redteam.Informe
		if err := esperarPlan(fmt.Sprintf(idioma.T("redteam · atacando %s"), t.ID), func(func(string)) error {
			var err error
			inf, err = atacarTarea(context.Background(), root, cfg, t, gen, conservar || reabrir)
			return err
		}); err != nil {
			out.Line(idioma.T("· %s · no se pudo atacar · %v"), t.ID, err)
			continue
		}
		informes[t.ID] = inf
		imprimirInforme(inf)
	}
	if err := out.Data(informes); err != nil {
		return err
	}
	if !reabrir {
		return nil
	}
	return reabrirConfirmados(root, objetivo, informes)
}

func generadorRedteam(root string, cfg config.Config) (redteam.Generador, error) {
	ex, err := elegirEjecutor(cfg.Cli)
	if err != nil {
		return nil, err
	}
	return generadorPlan{ex: ex, modelo: config.ModeloRol(cfg, "planificador"), root: root}, nil
}

// atacarTarea builds the surface the attacker may stand on and runs the attack.
func atacarTarea(ctx context.Context, root string, cfg config.Config, t task.Task, gen redteam.Generador, conservar bool) (redteam.Informe, error) {
	ref := "HEAD"
	if room.RamaExiste(ctx, root, t.ID) {
		ref = room.Branch(t.ID)
	}
	s := redteam.Superficie{Requisitos: map[string]string{}, Diff: diffDeTarea(root, cfg.Base, ref)}
	if ruta, err := spec.Find(root); err == nil {
		if sp, err := spec.Load(ruta); err == nil {
			for i, id := range sp.IDs() {
				s.Requisitos[id] = sp.Requirements[i]
			}
			s.Reglas = sp.Reglas
			for _, a := range sp.Acceptance {
				s.Aceptacion = append(s.Aceptacion, a.Criterion)
			}
		}
	}
	s.DNA, _ = dna.Bloque(config.Dir(root), cfg.DNA)
	o := redteam.Opciones{Root: root, Tarea: t, Ref: ref, Superficie: s, Gen: gen}
	if conservar {
		o.Conservar = filepath.Join(config.Dir(root), "redteam")
	}
	return redteam.Atacar(ctx, o)
}

func diffDeTarea(root, base, ref string) string {
	if base == "" || ref == "HEAD" {
		return ""
	}
	b, err := exec.Command("git", "-C", root, "diff", base+"..."+ref).Output()
	if err != nil {
		return ""
	}
	if len(b) > 20000 {
		b = b[:20000]
	}
	return string(b)
}

func imprimirInforme(inf redteam.Informe) {
	if inf.Omitido != "" {
		out.Line(idioma.T("· %s · redteam omitido · %s"), inf.Tarea, inf.Omitido)
		return
	}
	out.Line(idioma.T("redteam · %s · %d ataques"), inf.Tarea, len(inf.Resultados))
	for _, r := range inf.Resultados {
		g := map[redteam.Clase]string{redteam.Confirmado: "✗", redteam.Afirmacion: "?", redteam.FallaAtacante: "·", redteam.Resistio: "✓"}[r.Clase]
		out.Line("  %s %-14s %-16s %s %s", g, r.Clase, r.Ataque.Invariante, r.Ataque.Objetivo, r.Detalle)
	}
	if len(inf.Descartados) > 0 {
		out.Line(idioma.T("  · %d ataques descartados por no apoyarse en nada real"), len(inf.Descartados))
	}
}

// pasoRedteam is the exit-gate step. Anything that goes wrong on the
// attacker's side skips the step: only a confirmed crash can stop a delivery.
func pasoRedteam(root string, cfg config.Config, t task.Task, modo string) func(context.Context) (string, bool) {
	return func(ctx context.Context) (string, bool) {
		gen, err := generadorRedteam(root, cfg)
		if err != nil {
			return fmt.Sprintf(idioma.T("redteam omitido · %v"), err), true
		}
		inf, err := atacarTarea(ctx, root, cfg, t, gen, false)
		if err != nil {
			return fmt.Sprintf(idioma.T("redteam omitido · %v"), err), true
		}
		if inf.Omitido != "" {
			return idioma.T("redteam omitido · ") + inf.Omitido, true
		}
		claims := 0
		for _, r := range inf.Resultados {
			if r.Clase == redteam.Afirmacion {
				claims++
			}
		}
		conf := inf.Confirmados()
		det := fmt.Sprintf(idioma.T("%d ataques · %d confirmados · %d afirmaciones · %d descartados"), len(inf.Resultados), len(conf), claims, len(inf.Descartados))
		if len(conf) == 0 {
			return det, true
		}
		det += " · " + conf[0].Ataque.Objetivo + ": " + conf[0].Detalle + " · loom redteam " + t.ID + " --reopen"
		return det, modo != "block"
	}
}

// reabrirConfirmados reopens the owner of every confirmed crash with the
// hostile test written into its contract, then runs it.
func reabrirConfirmados(root string, objetivo []task.Task, informes map[string]redteam.Informe) error {
	reabiertas, err := reabrirContratos(root, objetivo, informes)
	if err != nil {
		return err
	}
	if reabiertas == 0 {
		out.Line(idioma.T("· ningún crash confirmado que reabrir"))
		return nil
	}
	return runCmd(1, "", "", true, false)
}

// reabrirContratos rewrites the contract and the state of each owner; it runs nothing.
func reabrirContratos(root string, objetivo []task.Task, informes map[string]redteam.Informe) (int, error) {
	reabiertas := 0
	dir := config.TasksDir(root)
	for _, t := range objetivo {
		conf := informes[t.ID].Confirmados()
		if len(conf) == 0 {
			continue
		}
		st, err := state.Get(root, t.ID)
		if err != nil || st.Estado != state.Lista {
			out.Line(idioma.T("· %s tiene un crash confirmado pero no está lista · arréglala con loom run"), t.ID)
			continue
		}
		leng := "go"
		if strings.HasSuffix(strings.TrimSuffix(t.TocarSolo[0], "**"), ".py") {
			leng = "python"
		}
		if err := task.Save(dir, redteam.Reabrir(t, conf[0], leng)); err != nil {
			return reabiertas, err
		}
		if err := state.Save(root, state.State{ID: t.ID, Estado: state.Pendiente, Rama: st.Rama, Puerto: st.Puerto, Commit: st.Commit}); err != nil {
			return reabiertas, err
		}
		out.Line(idioma.T("· %s reabierta con el ataque de %s en su contrato · se reusa su cuarto"), t.ID, conf[0].Ataque.Invariante)
		reabiertas++
	}
	return reabiertas, nil
}
