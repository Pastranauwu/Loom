package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// estadoRepo es lo que `loom status` resume en una línea: dónde está la
// base, qué lleva la integración y qué falta entregar.
type estadoRepo struct {
	Rama        string   `json:"rama"` // rama actual del repo principal
	Base        string   `json:"base"`
	BaseTip     string   `json:"base_tip,omitempty"`
	IntegraTip  string   `json:"integra_tip,omitempty"`
	IntegraMas  int      `json:"integra_sobre_base,omitempty"`
	SinEntregar []string `json:"sin_entregar"`
	Pendientes  []string `json:"pendientes,omitempty"`
	Detenidas   []string `json:"detenidas,omitempty"`
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "una línea: rama base, punta de la integración y tareas sin entregar",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			e, err := leerEstadoRepo(root)
			if err != nil {
				return err
			}
			if err := out.Data(e); err != nil {
				return err
			}
			out.Line("%s", e.String())
			return nil
		},
	}
}

func leerEstadoRepo(root string) (estadoRepo, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return estadoRepo{}, err
	}
	git := func(args ...string) string {
		s, err := gitEn(root, args...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(s)
	}
	e := estadoRepo{Rama: git("branch", "--show-current"), Base: cfg.Base, SinEntregar: []string{}}
	e.BaseTip = git("rev-parse", "--short", cfg.Base)
	if tip := git("rev-parse", "--short", "--verify", "--quiet", room.IntegrationBranch); tip != "" {
		e.IntegraTip = tip
		e.IntegraMas, _ = strconv.Atoi(git("rev-list", "--count", cfg.Base+".."+room.IntegrationBranch))
	}
	tareas, err := task.List(config.TasksDir(root))
	if err != nil {
		return e, err
	}
	ctx := context.Background()
	entregadas := ship.Entregadas(ctx, root, cfg.Base)
	for _, t := range tareas {
		st, err := state.Get(root, t.ID)
		if err != nil {
			continue
		}
		switch st.Estado {
		case state.Lista:
			if !entregadas[t.ID] && room.RamaExiste(ctx, root, t.ID) {
				e.SinEntregar = append(e.SinEntregar, t.ID)
			}
		case state.Detenida:
			e.Detenidas = append(e.Detenidas, t.ID)
		case state.Pendiente, state.EnCurso:
			e.Pendientes = append(e.Pendientes, t.ID)
		}
	}
	return e, nil
}

func (e estadoRepo) String() string {
	partes := []string{"base " + e.Base + "@" + e.BaseTip}
	if e.Rama != "" && e.Rama != e.Base {
		partes = append(partes, "estás en "+e.Rama)
	}
	if e.IntegraTip != "" {
		partes = append(partes, "_integra "+e.IntegraTip+" (+"+strconv.Itoa(e.IntegraMas)+")")
	} else {
		partes = append(partes, "sin _integra")
	}
	if len(e.SinEntregar) > 0 {
		partes = append(partes, "sin entregar: "+strings.Join(e.SinEntregar, ", "))
	} else {
		partes = append(partes, "nada sin entregar")
	}
	if len(e.Pendientes) > 0 {
		partes = append(partes, "pendientes: "+strings.Join(e.Pendientes, ", "))
	}
	if len(e.Detenidas) > 0 {
		partes = append(partes, "detenidas: "+strings.Join(e.Detenidas, ", "))
	}
	return strings.Join(partes, " · ")
}
