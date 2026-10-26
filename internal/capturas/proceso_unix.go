//go:build !windows

package capturas

import (
	"os/exec"
	"syscall"
)

// enGrupo pone la app en su propio grupo: "levantar" suele ser un script
// que lanza hijos (vite, uvicorn), y matar solo al sh los dejaría vivos.
func enGrupo(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	morirConPadre(c.SysProcAttr)
}

func apagar(c *exec.Cmd) {
	if c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		_, _ = c.Process.Wait()
	}
}
