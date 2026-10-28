package task

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// Sandbox es "auto" (o vacío) u "off". Lo fija cmd/loom con el sandbox: de
// config.yml antes de correr nada.
var Sandbox = ""

// Aviso recibe, una vez por proceso, por qué las pruebas corren sin
// aislamiento. Lo cablea cmd/loom a su salida.
var Aviso = func(s string) { fmt.Fprintln(os.Stderr, s) }

var (
	sondaOnce sync.Once
	bwrapRuta string
	estado    = "apagado"
)

// EstadoSandbox: activo, ausente, bloqueado o apagado.
func EstadoSandbox() string {
	bwrap()
	return estado
}

// bwrap devuelve la ruta de bwrap si se puede usar, o "". La sonda corre una
// vez: en un Ubuntu con user namespaces bloqueados bwrap existe pero falla.
func bwrap() string {
	if strings.EqualFold(strings.TrimSpace(Sandbox), "off") {
		estado = "apagado"
		return ""
	}
	sondaOnce.Do(func() {
		ruta, err := exec.LookPath("bwrap")
		if err != nil {
			estado = "ausente"
			Aviso(idioma.T("· bwrap no está instalado · las pruebas corren sin aislamiento en su cuarto"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, ruta, "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--unshare-net", "true").Run(); err != nil {
			estado = "bloqueado"
			Aviso(idioma.T("· bwrap no puede crear el sandbox (user namespaces bloqueados) · las pruebas corren sin aislamiento en su cuarto"))
			return
		}
		bwrapRuta, estado = ruta, "activo"
	})
	return bwrapRuta
}

// ArgsBwrap arma el sandbox de un comando de verificación: la raíz en solo
// lectura, /tmp limpio, el cuarto en lectura y escritura, el .git común si
// el cuarto es un worktree, las cachés del usuario para no volver a bajar
// dependencias y, salvo que la tarea pida red, solo loopback.
func ArgsBwrap(dir string, red bool) []string {
	args := []string{"--die-with-parent", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp"}
	vistos := map[string]bool{}
	bind := func(p string) {
		if p == "" || vistos[p] {
			return
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			return
		}
		vistos[p] = true
		args = append(args, "--bind", p, p)
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, c := range []string{".cache", ".npm", ".cargo", "go", ".pnpm-store", ".yarn"} {
			bind(filepath.Join(home, c))
		}
	}
	for _, v := range []string{"GOCACHE", "GOMODCACHE", "CARGO_HOME", "npm_config_cache", "PIP_CACHE_DIR"} {
		bind(os.Getenv(v))
	}
	// lo que el PATH trae desde /tmp (toolchains de CI, binarios de prueba)
	// quedaría tapado por el tmpfs: se ve, pero no se escribe
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.HasPrefix(p, "/tmp/") && !vistos[p] {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				vistos[p] = true
				args = append(args, "--ro-bind", p, p)
			}
		}
	}
	bind(gitComun(dir))
	bind(dir)
	args = append(args, "--chdir", dir)
	if !red {
		args = append(args, "--unshare-net")
	}
	return args
}

// gitComun es el .git del repo principal cuando dir es un worktree (su .git
// es un archivo "gitdir: <repo>/.git/worktrees/<n>"); "" si no lo es.
func gitComun(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return ""
	}
	g, ok := strings.CutPrefix(strings.TrimSpace(string(b)), "gitdir:")
	if !ok {
		return ""
	}
	g = strings.TrimSpace(g)
	if !filepath.IsAbs(g) {
		g = filepath.Join(dir, g)
	}
	if filepath.Base(filepath.Dir(g)) == "worktrees" {
		return filepath.Dir(filepath.Dir(g))
	}
	return ""
}

// Shell prepara un comando de verificación (listo_cuando, pruebas,
// aceptación) en dir: sin terminal (SinTerminal) y, si bwrap sirve, en su
// sandbox. red deja la red abierta.
func Shell(ctx context.Context, dir, cmd string, red bool) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return Argv(ctx, dir, red, "cmd", "/c", SinTerminal(cmd))
	}
	return Argv(ctx, dir, red, "sh", "-c", SinTerminal(cmd))
}

// Argv es Shell para un comando ya partido en argumentos.
func Argv(ctx context.Context, dir string, red bool, name string, args ...string) *exec.Cmd {
	var c *exec.Cmd
	if b := bwrap(); b != "" && dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		c = exec.CommandContext(ctx, b, append(append(ArgsBwrap(dir, red), "--", name), args...)...)
	} else {
		c = exec.CommandContext(ctx, name, args...)
	}
	c.Dir = dir
	return c
}
