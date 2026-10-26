package executor

import "syscall"

// morirConPadre hace que el kernel mate al agente si loom muere. Sin
// esto, matar loom (kill, OOM, o una corrida --fondo) dejaba al
// `claude -p` del arquitecto corriendo solo y gastando la cuota.
func morirConPadre() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
