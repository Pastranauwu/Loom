package ship

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
)

// archivoPuntas guarda, mientras dura una entrega conjunta, la punta que
// tenía la rama de cada cuarto antes de que la esclusa la aplanara.
func archivoPuntas(root string) string { return filepath.Join(config.Dir(root), "puntas.json") }

func guardarPuntas(root string, puntas map[string]string) {
	if b, err := json.Marshal(puntas); err == nil {
		_ = os.WriteFile(archivoPuntas(root), b, 0o644)
	}
}

// RestaurarPuntas devuelve a su punta las ramas que una entrega cortada
// dejó aplanadas. EntregarTodas las restaura al salir, pero un Ctrl+C o un
// kill no corre ese paso, y con la rama reescrita `run` ya no puede juntar
// las tareas que dependen de ella. Solo mueve la rama si su contenido es
// el mismo que el de la punta guardada y no hay cambios sin commitear:
// aplanar no cambia el árbol, así que cualquier diferencia es trabajo
// posterior y se deja. Devuelve los cuartos restaurados y los que no tocó.
func RestaurarPuntas(root string) (restauradas, dudosas []string) {
	b, err := os.ReadFile(archivoPuntas(root))
	if err != nil {
		return nil, nil
	}
	var puntas map[string]string
	_ = json.Unmarshal(b, &puntas)
	for path, punta := range puntas {
		_, _ = gitRun(path, "rebase", "--abort")
		actual, err := gitRun(path, "rev-parse", "HEAD")
		if err != nil || strings.TrimSpace(actual) == punta {
			continue
		}
		_, errArbol := gitRun(path, "diff", "--quiet", punta, "HEAD")
		_, errSucio := gitRun(path, "diff", "--quiet", "HEAD")
		if errArbol != nil || errSucio != nil {
			dudosas = append(dudosas, filepath.Base(path))
			continue
		}
		if _, err := gitRun(path, "reset", "--hard", punta); err == nil {
			restauradas = append(restauradas, filepath.Base(path))
		}
	}
	_ = os.Remove(archivoPuntas(root))
	sort.Strings(restauradas)
	sort.Strings(dudosas)
	return restauradas, dudosas
}

// avisoPuntas es el paso que cuenta lo que RestaurarPuntas encontró.
func avisoPuntas(restauradas, dudosas []string) Paso {
	detalle := "una entrega anterior se cortó"
	if len(restauradas) > 0 {
		detalle += " · ramas devueltas a su punta: " + strings.Join(restauradas, ", ")
	}
	if len(dudosas) > 0 {
		detalle += " · con trabajo posterior, no se tocaron: " + strings.Join(dudosas, ", ")
	}
	return Paso{"ramas", true, detalle}
}

// AvisoPuntas es avisoPuntas como texto, para quien no arma pasos.
func AvisoPuntas(restauradas, dudosas []string) string {
	return avisoPuntas(restauradas, dudosas).Detalle
}
