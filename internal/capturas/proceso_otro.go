//go:build !linux && !windows

package capturas

import "syscall"

// ponytail: fuera de Linux no hay Pdeathsig; la app sobrevive a un
// loom matado.
func morirConPadre(*syscall.SysProcAttr) {}
