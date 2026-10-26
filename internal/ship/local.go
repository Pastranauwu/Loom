package ship

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
)

// PRLocal marca el PR de un repo sin remoto: la rama se queda en el repo y
// la descripción que llevaría el PR va a .loom/pr/. Un repo local es un
// caso normal, no un error: la esclusa corre igual y lo único que cambia es
// dónde queda la entrega.
const PRLocal = "local · "

// sinRemoto reporta si el repo no tiene origin, y por tanto el PR es local.
func sinRemoto(root string) bool {
	_, err := gitRun(root, "remote", "get-url", "origin")
	return err != nil
}

// prLocal reporta si el PR se queda en el repo: porque no hay origin o
// porque el humano lo pidió (`entrega: local`, --local).
func prLocal(root string, cfg config.Config) bool {
	return cfg.Entrega == config.EntregaLocal || sinRemoto(root)
}

// archivoPRLocal devuelve dónde vive la descripción del PR local de una rama.
func archivoPRLocal(root, rama string) string {
	return filepath.Join(config.Dir(root), "pr", strings.TrimPrefix(rama, "loom/")+".md")
}

// abrirPRLocal escribe la descripción del PR junto a la rama, que se queda
// tal cual para revisarla y mergearla con git. Devuelve la referencia que
// ocupa el lugar de la URL.
func abrirPRLocal(root, rama, base, titulo, cuerpo string) (string, error) {
	archivo := archivoPRLocal(root, rama)
	if err := os.MkdirAll(filepath.Dir(archivo), 0o755); err != nil {
		return "", err
	}
	texto := fmt.Sprintf("# %s\n\n`%s` → `%s`\n\n%s", titulo, rama, base, cuerpo)
	if err := os.WriteFile(archivo, []byte(texto), 0o644); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, archivo)
	if err != nil {
		rel = archivo
	}
	return PRLocal + rama + " · " + rel, nil
}

// comentarPRLocal anexa el informe del revisor a la descripción del PR
// local, que es donde lo va a leer quien aprueba.
func comentarPRLocal(root, rama, informe string) error {
	f, err := os.OpenFile(archivoPRLocal(root, rama), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("\n---\n\n" + informe + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// integrarLocal lleva la rama de entrega a la base por fast-forward, que
// es lo que un merge por rebase deja en un PR remoto: un commit por tarea,
// sin commit de merge. Si la base avanzó, realinea una vez y reintenta.
func integrarLocal(ctx context.Context, root, path, base string) error {
	avanzar := func() (string, error) {
		// con la base en la carpeta de trabajo, merge actualiza también los
		// archivos y se niega si pisaría cambios sin commitear; fuera de
		// ella basta mover la ref, y fetch se niega si no es fast-forward
		if actual, _ := gitRun(root, "symbolic-ref", "--quiet", "--short", "HEAD"); strings.TrimSpace(actual) == base {
			return gitRun(root, "merge", "--ff-only", RamaEntrega)
		}
		return gitRun(root, "fetch", ".", RamaEntrega+":"+base)
	}
	salida, err := avanzar()
	if err == nil {
		return nil
	}
	if err := realinearConBase(ctx, root, path, base, false); err != nil {
		return fmt.Errorf("%s · %s", tail(salida), err)
	}
	if salida, err = avanzar(); err != nil {
		return fmt.Errorf("no se pudo integrar en %s · %s", base, tail(salida))
	}
	return nil
}
