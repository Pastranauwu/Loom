package room

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// paqueteFalso deja en el .venv de dir un paquete instalado a mano, que
// no existe en ningún registro: solo se consigue heredándolo.
func paqueteFalso(t *testing.T, dir, nombre string) {
	t.Helper()
	py := filepath.Join(dir, ".venv", binVenv(), "python")
	if !exists(py) {
		if out, err := exec.Command(pythonSistema(), "-m", "venv", filepath.Join(dir, ".venv")).CombinedOutput(); err != nil {
			t.Fatalf("venv: %s", out)
		}
	}
	sp, err := exec.Command(py, "-c", "import sysconfig; print(sysconfig.get_path('purelib'))").Output()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSpace(string(sp))
	for archivo, texto := range map[string]string{
		filepath.Join(nombre, "__init__.py"):                "",
		filepath.Join(nombre+"-1.0.dist-info", "METADATA"):  "Metadata-Version: 2.1\nName: " + nombre + "\nVersion: 1.0\n",
		filepath.Join(nombre+"-1.0.dist-info", "INSTALLER"): "loom\n",
	} {
		ruta := filepath.Join(base, archivo)
		if err := os.MkdirAll(filepath.Dir(ruta), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ruta, []byte(texto), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func importa(dir, modulo string) bool {
	return exec.Command(filepath.Join(dir, ".venv", binVenv(), "python"), "-c", "import "+modulo).Run() == nil
}

func requirements(t *testing.T, dir, texto, mensaje string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(texto), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "requirements.txt")
	gitCmd(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", mensaje)
}

// El cuarto hereda lo que el repo principal ya tiene instalado, y el
// cuarto de una tarea, además, lo del cuarto de cuyo trabajo parte. Ningún
// paquete de la prueba existe en un registro: si pip intentara instalarlos
// de cero, Create fallaría.
func TestCuartoHeredaElEntornoDelRepoYDelCuartoBase(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	ctx := context.Background()
	root := repoConCommit(t)
	paqueteFalso(t, root, "dcprincipal")
	requirements(t, root, "dcprincipal==1.0\n", "manifiesto")

	esq, err := Create(ctx, root, "T-001", "main")
	if err != nil {
		t.Fatalf("el cuarto no heredó el entorno del repo · %v", err)
	}
	if !importa(esq.Path, "dcprincipal") {
		t.Fatal("el cuarto no ve los paquetes del repo principal")
	}
	// el esqueleto agrega una dependencia y la instala en su cuarto
	paqueteFalso(t, esq.Path, "dcesqueleto")
	requirements(t, esq.Path, "dcprincipal==1.0\ndcesqueleto==1.0\n", "wip: T-001 esqueleto")

	tarea, err := Create(ctx, root, "T-002", esq.Rama)
	if err != nil {
		t.Fatalf("la tarea no heredó el entorno del esqueleto · %v", err)
	}
	if !importa(tarea.Path, "dcesqueleto") || !importa(tarea.Path, "dcprincipal") {
		t.Error("la tarea no ve lo que instaló el esqueleto")
	}
	// un cuarto que no parte del esqueleto no hereda de él
	otra, err := Create(ctx, root, "T-003", "main")
	if err != nil {
		t.Fatal(err)
	}
	if importa(otra.Path, "dcesqueleto") {
		t.Error("T-003 parte de main y no debe ver los paquetes del esqueleto")
	}
	// la rama de entrega trae el trabajo por cherry-pick: ningún cuarto
	// es su ancestro y sin nombrarlos instalaría de cero
	if err := InstalarDependencias(ctx, otra.Path, "T-001"); err != nil {
		t.Fatal(err)
	}
	if !importa(otra.Path, "dcesqueleto") {
		t.Error("con heredarDe, T-003 debe ver los paquetes del cuarto nombrado")
	}
}

// Con un entorno heredado de otro cuarto, que pip no instale lo que falta
// no detiene la tarea: decide listo_cuando y el motivo queda en runs/.
func TestInstalacionFallidaConEntornoHeredadoNoEsFatal(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("sin python3")
	}
	ctx := context.Background()
	root := repoConCommit(t)
	requirements(t, root, "", "manifiesto")
	esq, err := Create(ctx, root, "T-001", "main")
	if err != nil {
		t.Fatal(err)
	}
	requirements(t, esq.Path, "paquete-que-no-existe-loom==9.9.9\n", "wip: T-001 esqueleto")

	if _, err := Create(ctx, root, "T-002", esq.Rama); err != nil {
		t.Fatalf("debía seguir con el entorno heredado · %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".loom", "runs", "T-002", "entorno.log")); err != nil {
		t.Error("falta el rastro de la instalación fallida en runs/T-002/entorno.log")
	}
	// sin cuarto del que heredar sigue siendo un error
	requirements(t, root, "paquete-que-no-existe-loom==9.9.9\n", "roto")
	if _, err := Create(ctx, root, "T-009", "main"); err == nil {
		t.Error("sin entorno verificado del que heredar, la instalación fallida detiene el cuarto")
	}
}
