package capturas

import "syscall"

// morirConPadre hace que el kernel mate la app si loom muere. Está en
// su propio grupo, así que el Ctrl-C de la terminal no le llega: sin esto
// quedaba sirviendo sola y la corrida siguiente no podía levantar la suya
// (puerto o base de prueba ocupados).
// ponytail: solo muere el proceso que lanzó "levantar"; los hijos que él
// haya lanzado sobreviven a un loom matado. Un cgroup lo cubriría.
func morirConPadre(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
