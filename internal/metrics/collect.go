package metrics

import (
	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/state"
	"github.com/Pastranauwu/loom/internal/task"
)

// Recoger lee del disco todo lo que Calcular necesita: contratos,
// estados, attempts.jsonl y entregas.
func Recoger(root string) (Datos, error) {
	tasks, err := task.List(config.TasksDir(root))
	if err != nil {
		return Datos{}, err
	}
	estados, err := state.List(root)
	if err != nil {
		return Datos{}, err
	}
	entregas, err := ListarEntregas(root)
	if err != nil {
		return Datos{}, err
	}

	porID := make(map[string]state.State, len(estados))
	for _, s := range estados {
		porID[s.ID] = s
	}
	attempts := make(map[string][]loop.Attempt)
	for _, t := range tasks {
		as, err := loop.ReadAttempts(root, t.ID)
		if err != nil {
			return Datos{}, err
		}
		attempts[t.ID] = as
	}

	return Datos{Tasks: tasks, Estados: porID, Attempts: attempts, Entregas: entregas}, nil
}
