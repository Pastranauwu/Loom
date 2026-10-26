package ship

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/room"
	"github.com/Pastranauwu/loom/internal/task"
)

// verificarBisectable corre el comando de pruebas del proyecto sobre el
// commit aplanado: cada commit debe compilar y pasar pruebas.
// Con un solo commit, es correr la suite una vez y exigir verde.
func verificarBisectable(ctx context.Context, roomPath, pruebas string, timeout time.Duration) (string, bool) {
	if strings.TrimSpace(pruebas) == "" {
		return "sin comando de pruebas · decláralo en config.yml", false
	}
	salida, code := runComando(ctx, roomPath, pruebas, timeout)
	if code != nil && *code == 0 {
		return "verde · " + pruebas, true
	}
	return "falla " + pruebas + " · " + tail(salida), false
}

// suiteYaFallaba corre pruebas sobre el commit base de la tarea, en el
// mismo cuarto (sus dependencias instaladas sirven), y vuelve a la rama.
func suiteYaFallaba(ctx context.Context, roomPath, base, pruebas string, timeout time.Duration) bool {
	if strings.TrimSpace(pruebas) == "" || base == "" {
		return false
	}
	rama, err := exec.Command("git", "-C", roomPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(rama)) == "HEAD" {
		return false
	}
	if err := exec.Command("git", "-C", roomPath, "checkout", "--quiet", "--detach", base).Run(); err != nil {
		return false
	}
	defer exec.Command("git", "-C", roomPath, "checkout", "--quiet", strings.TrimSpace(string(rama))).Run()
	_, code := runComando(ctx, roomPath, pruebas, timeout)
	return code == nil || *code != 0
}

func runComando(ctx context.Context, dir, cmdStr string, timeout time.Duration) (string, *int) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", task.SinTerminal(cmdStr))
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", task.SinTerminal(cmdStr))
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), room.Entorno(dir)...)

	out, err := cmd.CombinedOutput()
	code := 0
	if ctx.Err() == context.DeadlineExceeded {
		code = 124
		return string(out), &code
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
			return string(out), &code
		}
		return string(out) + err.Error(), nil
	}
	return string(out), &code
}

// tail conserva la última línea útil de una salida para el motivo.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "sin salida"
	}
	lines := strings.Split(s, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" && len(lines) > 1 {
		last = strings.TrimSpace(lines[len(lines)-2])
	}
	const max = 160
	if len(last) > max {
		last = last[:max] + "…"
	}
	return last
}
