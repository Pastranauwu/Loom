package historial

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Pastranauwu/loom/internal/ship"
	"github.com/Pastranauwu/loom/internal/spec"
)

const specDePrueba = `feature: Recuperación de contraseña
motivo: soporte resetea 40 cuentas a mano por semana
requirements:
  - solicitar recuperación por email
acceptance:
  - token expirado debe rechazarse
  - criterion: integración completa
    command: go test ./...
`

func gitEn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func escribir(t *testing.T, path, contenido string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo deja un repositorio con el spec commiteado y una tarea, y la
// aceptación de la última entrega como la reciba.
func repo(t *testing.T, contenidoSpec string, a *ship.Aceptacion) (root, specPath string) {
	t.Helper()
	root = t.TempDir()
	gitEn(t, root, "init", "-q")
	gitEn(t, root, "config", "user.email", "t@t")
	gitEn(t, root, "config", "user.name", "t")
	specPath = filepath.Join(root, "loom.spec.yml")
	escribir(t, specPath, contenidoSpec)
	escribir(t, filepath.Join(root, ".loom", "tasks", "T-001.md"), "---\nversion: 1\nid: T-001\ntitulo: enviar el correo\nlisto_cuando: go test ./...\ntocar_solo: [\"mail/**\"]\n---\n")
	escribir(t, filepath.Join(root, ".loom", spec.IntencionFile), "{}")
	gitEn(t, root, "add", "-A")
	gitEn(t, root, "commit", "-q", "-m", "base")
	if a != nil {
		b, _ := json.Marshal(a)
		escribir(t, filepath.Join(root, ".loom", ship.AceptacionFile), string(b))
	}
	return root, specPath
}

// hashDe es el hash con que `ship --todas` anota el spec que probó.
func hashDe(t *testing.T, contenido string) string {
	t.Helper()
	s, err := spec.Parse([]byte(contenido))
	if err != nil {
		t.Fatal(err)
	}
	return spec.IntencionDe(s).Hash()
}

func verde() *ship.Aceptacion {
	s, _ := spec.Parse([]byte(specDePrueba))
	return &ship.Aceptacion{
		Spec:  spec.IntencionDe(s).Hash(),
		Fecha: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Rama: "loom/_entrega", Commit: "abc123", Aprobado: true,
		Tareas:    []string{"T-001"},
		Criterios: []ship.Criterio{{Comando: "go test ./...", Corrio: true, Paso: true}},
	}
}

func leer(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestArchivarTodoVerde(t *testing.T) {
	root, specPath := repo(t, specDePrueba, verde())
	e, err := Archivar(root, specPath)
	if err != nil {
		t.Fatal(err)
	}
	if e.Nombre != "0001-recuperacion-de-contrasena" {
		t.Errorf("entrada = %q", e.Nombre)
	}
	if got := leer(t, filepath.Join(e.Dir, "spec.yml")); got != specDePrueba {
		t.Errorf("spec.yml no es copia exacta:\n%s", got)
	}
	res := leer(t, filepath.Join(e.Dir, "resultado.yml"))
	for _, quiero := range []string{"estado: probado", "fecha: \"2026-10-01\"", "commit: abc123", "resultado: pasó", "resultado: sin comando", "criterio: integración completa", "id: T-001", "titulo: enviar el correo", "intentos: 0"} {
		if !strings.Contains(res, quiero) {
			t.Errorf("resultado.yml sin %q:\n%s", quiero, res)
		}
	}
	if idx := leer(t, Index(root)); !strings.Contains(idx, "| 2026-10-01 | 0001 | Recuperación de contraseña | soporte resetea 40 cuentas a mano por semana | probado |") {
		t.Errorf("index.md:\n%s", idx)
	}
	if got := leer(t, specPath); got != "" {
		t.Errorf("el spec debe quedar en 0 bytes, quedó %q", got)
	}
	for _, f := range []string{spec.IntencionFile, spec.FeatureStateFile, ship.AceptacionFile} {
		if _, err := os.Stat(filepath.Join(root, ".loom", f)); !os.IsNotExist(err) {
			t.Errorf("%s debía borrarse: es del feature archivado", f)
		}
	}
	if msg := gitEn(t, root, "log", "-1", "--format=%s"); msg != "loom: archiva 0001-recuperacion-de-contrasena" {
		t.Errorf("commit = %q", msg)
	}
	if sucio := gitEn(t, root, "status", "--porcelain"); sucio != "" {
		t.Errorf("quedó sin commitear:\n%s", sucio)
	}
	// archivado y sin entrega nueva: no hay nada que archivar otra vez
	if _, err := Archivar(root, specPath); err == nil || !strings.Contains(err.Error(), "spec vacío") {
		t.Errorf("un spec ya archivado no se archiva dos veces: %v", err)
	}
}

func TestArchivarRechazaLoNoProbado(t *testing.T) {
	fallido := verde()
	fallido.Criterios[0].Paso = false
	cortada := verde()
	cortada.Criterios[0].Corrio = false
	frenada := verde()
	frenada.Aprobado, frenada.Motivo = false, "T-001 · ruido"

	for nombre, c := range map[string]struct {
		a      *ship.Aceptacion
		quiero string
	}{
		"criterio fallido":       {fallido, "criterio de aceptación sin pasar · go test ./..."},
		"criterio que no corrió": {cortada, "criterio de aceptación sin pasar"},
		"entrega frenada":        {frenada, "la última entrega no pasó · T-001 · ruido"},
		"sin entrega":            {nil, "sin entrega conjunta registrada"},
	} {
		root, specPath := repo(t, specDePrueba, c.a)
		_, err := Archivar(root, specPath)
		if err == nil || !strings.Contains(err.Error(), c.quiero) {
			t.Errorf("%s: err = %v, quiero %q", nombre, err, c.quiero)
		}
		if _, err := os.Stat(Dir(root)); !os.IsNotExist(err) {
			t.Errorf("%s: no debía escribir nada en el historial", nombre)
		}
		if leer(t, specPath) != specDePrueba {
			t.Errorf("%s: el spec no se toca si no se archiva", nombre)
		}
	}
}

func TestArchivarSpecVacio(t *testing.T) {
	for _, contenido := range []string{"", "  \n\n"} {
		root, specPath := repo(t, contenido, verde())
		if _, err := Archivar(root, specPath); err == nil || !strings.Contains(err.Error(), "spec vacío") {
			t.Errorf("spec %q: err = %v", contenido, err)
		}
		if _, err := os.Stat(Dir(root)); !os.IsNotExist(err) {
			t.Error("un spec vacío no crea entrada")
		}
	}
}

func TestArchivarNumeraEnOrdenSinReusar(t *testing.T) {
	root, specPath := repo(t, specDePrueba, verde())
	// ya hay historial, con un hueco: el siguiente es el mayor más uno
	for _, d := range []string{"0001-login", "0007-exportar-csv"} {
		if err := os.MkdirAll(filepath.Join(Dir(root), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e, err := Archivar(root, specPath)
	if err != nil || !strings.HasPrefix(e.Nombre, "0008-") {
		t.Fatalf("entrada = %q err = %v, quiero 0008-", e.Nombre, err)
	}

	escribir(t, specPath, "feature: \"¡¡Otro!! feature\"\nrequirements:\n  - algo\n")
	otra := verde()
	otra.Spec = hashDe(t, leer(t, specPath))
	b, _ := json.Marshal(otra)
	escribir(t, filepath.Join(root, ".loom", ship.AceptacionFile), string(b))
	e, err = Archivar(root, specPath)
	if err != nil || e.Nombre != "0009-otro-feature" {
		t.Fatalf("entrada = %q err = %v, quiero 0009-otro-feature", e.Nombre, err)
	}
	if n := strings.Count(leer(t, Index(root)), "| probado |"); n != 2 {
		t.Errorf("index.md tiene %d líneas, quiero 2 (no se borran)", n)
	}
	for _, d := range []string{"0001-login", "0007-exportar-csv"} {
		if _, err := os.Stat(filepath.Join(Dir(root), d)); err != nil {
			t.Errorf("la entrada %s desapareció", d)
		}
	}
}

// El proyecto real ignora `.loom/` entero: el commit de archive tiene
// que llevar el historial igual, y nada más de esa carpeta.
func TestArchivarEnRepoQueIgnoraLoom(t *testing.T) {
	root, specPath := repo(t, specDePrueba, verde())
	escribir(t, filepath.Join(root, ".gitignore"), "node_modules/\n.loom/\ndist/\n")
	gitEn(t, root, "rm", "-r", "-q", "--cached", ".loom")
	gitEn(t, root, "add", ".gitignore")
	gitEn(t, root, "commit", "-q", "-m", "ignora .loom")

	e, err := Archivar(root, specPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Gitignore) != 3 {
		t.Errorf("Archivar debe reportar las líneas que puso en .gitignore: %v", e.Gitignore)
	}
	stat := gitEn(t, root, "show", "--stat=200", "--format=%s")
	for _, quiero := range []string{
		"loom: archiva 0001-recuperacion-de-contrasena",
		".loom/historial/0001-recuperacion-de-contrasena/spec.yml",
		".loom/historial/0001-recuperacion-de-contrasena/resultado.yml",
		".loom/index.md",
		".gitignore",
		"loom.spec.yml",
	} {
		if !strings.Contains(stat, quiero) {
			t.Errorf("el commit no incluye %q:\n%s", quiero, stat)
		}
	}
	for _, no := range []string{ship.AceptacionFile, spec.IntencionFile, "tasks/"} {
		if strings.Contains(stat, no) {
			t.Errorf("%q no debe entrar al commit:\n%s", no, stat)
		}
	}
	if got := leer(t, filepath.Join(root, ".gitignore")); got != "node_modules/\n.loom/*\n!.loom/historial/\n!.loom/index.md\ndist/\n" {
		t.Errorf(".gitignore = %q", got)
	}
	// lo demás de .loom sigue ignorado: una aceptación nueva no ensucia
	escribir(t, filepath.Join(root, ".loom", ship.AceptacionFile), "{}")
	if sucio := gitEn(t, root, "status", "--porcelain"); sucio != "" {
		t.Errorf("el árbol quedó sucio:\n%s", sucio)
	}
}

// La entrega probó un spec concreto. Si después lo editan, lo que se
// archivaría como "probado" no es lo que pasó la aceptación.
func TestArchivarRechazaUnSpecQueCambioDesdeLaEntrega(t *testing.T) {
	root, specPath := repo(t, specDePrueba, verde())
	escribir(t, specPath, strings.Replace(specDePrueba, "solicitar recuperación por email", "solicitar recuperación por SMS", 1))
	if _, err := Archivar(root, specPath); err == nil || !strings.Contains(err.Error(), "el spec cambió desde la última entrega") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(Dir(root)); !os.IsNotExist(err) {
		t.Error("no debía escribir nada en el historial")
	}

	// el motivo y los comentarios no cambian lo que se construye: no
	// obligan a entregar de nuevo
	escribir(t, specPath, "# nota para el equipo\n"+strings.Replace(specDePrueba, "soporte resetea 40 cuentas a mano por semana", "otro motivo", 1))
	if _, err := Archivar(root, specPath); err != nil {
		t.Fatalf("un cambio de motivo no invalida la entrega: %v", err)
	}
	if !strings.Contains(leer(t, Index(root)), "otro motivo") {
		t.Error("el index lleva el motivo del spec que se archiva")
	}
}

// La regla que ignora el historial puede no estar en el .gitignore del
// proyecto: ahí archive no puede arreglarla y tiene que decir dónde está.
func TestArchivarAvisaSiElHistorialQuedaIgnoradoFueraDelProyecto(t *testing.T) {
	root, specPath := repo(t, specDePrueba, verde())
	escribir(t, filepath.Join(root, ".gitignore"), ".loom/\n")
	escribir(t, filepath.Join(root, ".git", "info", "exclude"), ".loom/\n")

	_, err := Archivar(root, specPath)
	if err == nil || !strings.Contains(err.Error(), "git ignora el historial") || !strings.Contains(err.Error(), "info/exclude") || !strings.Contains(err.Error(), "!.loom/historial/") {
		t.Fatalf("err = %v", err)
	}
	if leer(t, specPath) != specDePrueba {
		t.Error("el spec no se toca si no se puede archivar")
	}
	if got := leer(t, filepath.Join(root, ".gitignore")); got != ".loom/\n" {
		t.Errorf("el .gitignore debía quedar como estaba, quedó %q", got)
	}
	if _, err := os.Stat(Dir(root)); !os.IsNotExist(err) {
		t.Error("no debía escribir nada en el historial")
	}
}

func TestApartarDejaElTableroLimpio(t *testing.T) {
	root := t.TempDir()
	escribir(t, filepath.Join(root, ".loom", "tasks", "T-001.md"), "x")
	escribir(t, filepath.Join(root, ".loom", "tasks", "T-002.md"), "y")
	escribir(t, filepath.Join(root, ".loom", "state", "T-001.json"), "{}")
	escribir(t, filepath.Join(root, ".loom", "runs", "T-001", "attempts.jsonl"), "{}\n")

	n, err := Apartar(root, "0001-x", []string{"T-001", "T-009"})
	if err != nil || n != 1 {
		t.Fatalf("Apartar = %d, %v; quiero 1 tarea", n, err)
	}
	for _, p := range []string{"tasks/T-001.md", "state/T-001.json", "runs/T-001/attempts.jsonl"} {
		if _, err := os.Stat(filepath.Join(Apartados(root, "0001-x"), p)); err != nil {
			t.Errorf("falta %s apartado", p)
		}
		if _, err := os.Stat(filepath.Join(root, ".loom", p)); err == nil {
			t.Errorf("%s sigue en el tablero", p)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".loom", "tasks", "T-002.md")); err != nil {
		t.Error("T-002 no se entregó y no debe moverse")
	}
}
