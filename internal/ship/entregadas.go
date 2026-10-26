package ship

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TrailerTarea marca cada commit entregado con su tarea. Sobrevive al
// rebase y al squash de GitHub (que junta los mensajes), así loom
// reconoce en la rama base lo que ya entregó aunque el hash cambie.
const TrailerTarea = "Tarea"

// entrega es una línea de .loom/entregas.jsonl: la punta de una rama
// de entrega y las tareas que llevaba. Cubre los commits sin trailer
// (entregas anteriores a él) cuando la rama se integró con merge.
type entrega struct {
	Commit string   `json:"commit"`
	Tareas []string `json:"tareas"`
}

func rutaEntregas(root string) string {
	return filepath.Join(root, ".loom", "entregas.jsonl")
}

// RegistrarEntrega anota la punta de la rama de entrega y sus tareas.
func RegistrarEntrega(root, rama string, ids []string) error {
	commit, err := gitRun(root, "rev-parse", rama)
	if err != nil {
		return err
	}
	b, err := json.Marshal(entrega{Commit: strings.TrimSpace(commit), Tareas: ids})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(rutaEntregas(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Entregadas devuelve las tareas cuyo trabajo ya está en base: por el
// trailer Tarea de sus commits o porque la rama de una entrega
// registrada ya es ancestro de base. Sin esto, el estado `lista` no
// distingue "verde sin entregar" de "ya integrada", y cada ship --todas
// volvía a meter las tareas de entregas anteriores, que chocan con lo
// que vino después (T-001 contra las 28 que la rellenaron).
func Entregadas(ctx context.Context, root, base string) map[string]bool {
	out := map[string]bool{}
	if log, err := gitRun(root, "log", base, "--format=%(trailers:key="+TrailerTarea+",valueonly)"); err == nil {
		for _, id := range strings.Fields(log) {
			out[id] = true
		}
	}
	f, err := os.Open(rutaEntregas(root))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e entrega
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Commit == "" {
			continue
		}
		if _, err := gitRun(root, "merge-base", "--is-ancestor", e.Commit, base); err != nil {
			continue
		}
		for _, id := range e.Tareas {
			out[id] = true
		}
	}
	return out
}
