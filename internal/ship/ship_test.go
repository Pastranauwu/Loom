package ship

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repoConCommit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitCmd(t, root, "init", "-b", "main")
	gitCmd(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	return root
}

func escribir(t *testing.T, root, rel, contenido string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(contenido), 0o644); err != nil {
		t.Fatal(err)
	}
}

func taskTitulo(titulo string) task.Task {
	return task.Task{
		Version:        task.Version,
		ID:             "T-001",
		Titulo:         titulo,
		ListoCuando:    "true",
		LimiteIntentos: 3,
		LimiteLineas:   200,
	}
}

func cuartoConWip(t *testing.T, root string) room.Room {
	t.Helper()
	r, err := room.Create(context.Background(), root, "T-001", "main")
	if err != nil {
		t.Fatalf("room.Create: %v", err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, "T-001") })
	escribir(t, r.Path, "a.go", "package a\n")
	gitCmd(t, r.Path, "add", "-A")
	gitCmd(t, r.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip: T-001 intento 1")
	return r
}

func TestAplanar(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)

	cuenta, hash, err := aplanar(context.Background(), r.Path, "main", "T-001", "exportar a CSV", "feat", "glm-5.2")
	if err != nil {
		t.Fatalf("aplanar: %v", err)
	}
	if cuenta != 1 {
		t.Errorf("cuenta = %d, quiero 1", cuenta)
	}
	if hash == "" {
		t.Error("hash vacío")
	}
	msg := gitCmd(t, r.Path, "log", "-1", "--format=%B")
	if !strings.Contains(msg, "feat: exportar a CSV") {
		t.Errorf("mensaje = %q", msg)
	}
	if !strings.Contains(msg, "Agent: glm-5.2") || !strings.Contains(msg, "Tarea: T-001") {
		t.Errorf("sin trailers Agent y Tarea: %q", msg)
	}
	if n := strings.TrimSpace(gitCmd(t, r.Path, "rev-list", "--count", "main..HEAD")); n != "1" {
		t.Errorf("commits tras aplanar = %s, quiero 1", n)
	}
}

func TestRebaseSinRemoto(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)

	target, conflictos, err := rebase(context.Background(), root, r.Path, "main", r.Rama, false)
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if target != "main" {
		t.Errorf("target = %q, quiero main", target)
	}
	if len(conflictos) != 0 {
		t.Errorf("conflictos = %v", conflictos)
	}
}

// con --local y un origin que rechaza la credencial, git pedía usuario
// y contraseña una vez por tarea
func TestRebaseLocalNoTocaLaRed(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)
	// un origin que no existe y una origin/main ya traída: sin local, el
	// rebase iría sobre ella
	gitCmd(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "no-existe"))
	gitCmd(t, root, "update-ref", "refs/remotes/origin/main", "main")

	target, _, err := rebase(context.Background(), root, r.Path, "main", r.Rama, true)
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if target != "main" {
		t.Errorf("target = %q, quiero la base local", target)
	}
	if target, _, _ = rebase(context.Background(), root, r.Path, "main", r.Rama, false); target != "origin/main" {
		t.Errorf("sin local, target = %q, quiero origin/main", target)
	}
}

// GIT_TERMINAL_PROMPT=0 no apaga el askpass: el de un IDE abre su diálogo
// en otra ventana y el fetch quedaba colgado
func TestGitDeLaEntregaNoPregunta(t *testing.T) {
	root := repoConCommit(t)
	marca := filepath.Join(t.TempDir(), "preguntó")
	askpass := filepath.Join(t.TempDir(), "askpass.sh")
	if err := os.WriteFile(askpass, []byte("#!/bin/sh\ntouch "+marca+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_ASKPASS", askpass)
	t.Setenv("SSH_ASKPASS", askpass)
	pedir := func(env []string) error {
		cmd := exec.Command("git", "-c", "core.askPass=", "-c", "credential.helper=", "credential", "fill")
		cmd.Dir = root
		cmd.Env = env
		cmd.Stdin = strings.NewReader("protocol=https\nhost=ejemplo.invalid\n\n")
		return cmd.Run()
	}
	if err := pedir(sinPreguntas()); err == nil {
		t.Error("sin credencial ni a quién preguntar, git debía fallar")
	}
	if _, err := os.Stat(marca); err == nil {
		t.Fatal("git llamó al askpass")
	}
	// con el entorno tal cual sí lo llama: la prueba mide algo
	_ = pedir(os.Environ())
	if _, err := os.Stat(marca); err != nil {
		t.Error("el askpass de control no se llamó: la prueba no distingue nada")
	}
}

func TestRepoDeOrigin(t *testing.T) {
	for url, quiero := range map[string]string{
		"https://github.com/acme/app.git":   "acme/app",
		"https://github.com/acme/app":       "acme/app",
		"git@github.com:acme/app.git\n":     "acme/app",
		"ssh://git@github.com/acme/app.git": "acme/app",
		"https://gitlab.com/acme/app.git":   "",
		"/srv/git/app.git":                  "",
	} {
		if got := repoDeOrigin(url); got != quiero {
			t.Errorf("repoDeOrigin(%q) = %q, quiero %q", url, got, quiero)
		}
	}
}

func TestRebaseConflicto(t *testing.T) {
	root := repoConCommit(t)
	escribir(t, root, "f.txt", "base\n")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "f.txt base")

	r, err := room.Create(context.Background(), root, "T-001", "main")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = room.Destroy(context.Background(), root, "T-001") })

	// main avanza y el cuarto toca el mismo archivo
	escribir(t, root, "f.txt", "cambiado en main\n")
	gitCmd(t, root, "add", "-A")
	gitCmd(t, root, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "avanza main")

	escribir(t, r.Path, "f.txt", "cambiado en el cuarto\n")
	gitCmd(t, r.Path, "add", "-A")
	gitCmd(t, r.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip")

	_, conflictos, err := rebase(context.Background(), root, r.Path, "main", r.Rama, false)
	if err == nil {
		t.Fatal("rebase debió conflictuar")
	}
	if len(conflictos) == 0 || conflictos[0] != "f.txt" {
		t.Errorf("conflictos = %v, quiero [f.txt]", conflictos)
	}
}

func TestRunDryRun(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)
	cfg := config.Config{Base: "main", Pruebas: "true", PatronesPrueba: config.DefaultTestPatterns()}
	tk := taskTitulo("exportar a CSV")
	tk.TocarSolo = []string{"a.go"}

	res := Run(context.Background(), Opciones{
		Root: root, Room: r, Task: tk, Config: cfg, Modelo: "glm-5.2", Base: "main", DryRun: true,
	})
	if !res.Aprobado {
		t.Fatalf("dry-run no aprobado: %+v", res.Pasos)
	}
	if len(res.Pasos) != 9 {
		t.Fatalf("pasos = %d, quiero 9", len(res.Pasos))
	}
	nombres := []string{"base", "historial", "ruido", "secretos", "presupuesto", "interfaces", "bisectable", "handoff", "pr"}
	for i, n := range nombres {
		if res.Pasos[i].Nombre != n {
			t.Errorf("paso %d = %q, quiero %q", i, res.Pasos[i].Nombre, n)
		}
	}
}

func TestRunSeFrenaEnRuido(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)
	// añadir un print de debug al trabajo
	escribir(t, r.Path, "b.go", "package b\nfunc f() {\n\tfmt.Println(\"hola\")\n}\n")
	gitCmd(t, r.Path, "add", "-A")
	gitCmd(t, r.Path, "-c", "user.email=d@d", "-c", "user.name=d", "commit", "-m", "wip: T-001 intento 2")

	cfg := config.Config{Base: "main", Pruebas: "true", PatronesPrueba: config.DefaultTestPatterns()}
	tk := taskTitulo("exportar a CSV")

	res := Run(context.Background(), Opciones{Root: root, Room: r, Task: tk, Config: cfg, Modelo: "glm-5.2", Base: "main", DryRun: true})
	if res.Aprobado {
		t.Fatal("una tarea con print de debug debió frenarse en ruido")
	}
	ultimo := res.Pasos[len(res.Pasos)-1]
	if ultimo.Nombre != "ruido" || ultimo.OK {
		t.Errorf("último paso = %+v, quiero frenarse en ruido", ultimo)
	}
}

func TestOrdenTopologico(t *testing.T) {
	tareas := []task.Task{
		{ID: "T-003", DependeDe: []string{"T-001", "T-002"}},
		{ID: "T-001"},
		{ID: "T-002", DependeDe: []string{"T-001"}},
	}
	orden, err := ordenTopologico(tareas)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, t := range orden {
		pos[t.ID] = i
	}
	if len(orden) != 3 {
		t.Fatalf("orden = %d tareas, quiero 3", len(orden))
	}
	if pos["T-001"] > pos["T-002"] || pos["T-002"] > pos["T-003"] {
		t.Errorf("orden incorrecto: %v", pos)
	}

	// una dependencia fuera del lote ya vive en la base: no bloquea
	fuera := []task.Task{{ID: "T-009", DependeDe: []string{"T-000"}}}
	if _, err := ordenTopologico(fuera); err != nil {
		t.Errorf("dependencia fuera del lote no debe bloquear · %v", err)
	}

	// un ciclo se reporta, no se cuelga
	ciclo := []task.Task{
		{ID: "T-001", DependeDe: []string{"T-002"}},
		{ID: "T-002", DependeDe: []string{"T-001"}},
	}
	if _, err := ordenTopologico(ciclo); err == nil {
		t.Error("un ciclo debe reportarse")
	}
}

// Un --dry-run que deja la rama rebasada y aplanada no es en seco: la
// corrida siguiente ya no puede juntarla con las tareas de las que depende.
func TestRunConservarDejaLaRamaComoEstaba(t *testing.T) {
	root := repoConCommit(t)
	r := cuartoConWip(t, root)
	cfg := config.Config{Base: "main", Pruebas: "true", PatronesPrueba: config.DefaultTestPatterns()}
	tk := taskTitulo("exportar a CSV")
	tk.TocarSolo = []string{"a.go"}
	antes := strings.TrimSpace(gitCmd(t, r.Path, "rev-parse", "HEAD"))

	res := Run(context.Background(), Opciones{Root: root, Room: r, Task: tk, Config: cfg, Base: "main", DryRun: true, Conservar: true})
	if !res.Aprobado {
		t.Fatalf("dry-run no aprobado: %+v", res.Pasos)
	}
	if despues := strings.TrimSpace(gitCmd(t, r.Path, "rev-parse", "HEAD")); despues != antes {
		t.Errorf("la rama cambió de %s a %s", antes, despues)
	}
	if sucio := strings.TrimSpace(gitCmd(t, r.Path, "status", "--porcelain")); sucio != "" {
		t.Errorf("el cuarto quedó sucio:\n%s", sucio)
	}

	// sin Conservar (la entrega conjunta) el commit aplanado se queda
	Run(context.Background(), Opciones{Root: root, Room: r, Task: tk, Config: cfg, Base: "main", DryRun: true})
	if despues := strings.TrimSpace(gitCmd(t, r.Path, "rev-parse", "HEAD")); despues == antes {
		t.Error("sin Conservar la esclusa aplana la rama")
	}
}
