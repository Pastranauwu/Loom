//go:build !linux

package executor

import "syscall"

// ponytail: fuera de Linux no hay Pdeathsig; el agente sobrevive a un
// loom matado. Un grupo de procesos con kill al salir lo cubriría.
func morirConPadre() *syscall.SysProcAttr { return nil }
