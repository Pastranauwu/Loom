package examiner

import "github.com/Pastranauwu/loom/internal/task"

// Lo que el red team comparte con el examinador: el mismo criterio para
// decidir lenguaje, crate y toolchain, y la misma forma del archivo Rust.

// LenguajeDeTarea es el lenguaje de los archivos de la tarea, o el del repo.
func LenguajeDeTarea(detectado string, t task.Task) string { return lenguajeDeTarea(detectado, t) }

// CrateExaminable devuelve el crate de la tarea y su nombre de import;
// nombre vacío si no hay Cargo.toml o el crate no tiene biblioteca.
func CrateExaminable(root string, tocarSolo []string) (crate, nombre string) {
	return crateExaminable(root, tocarSolo)
}

// ArmarRust arma una prueba de integración que importa el crate.
func ArmarRust(crate string, imports, funcs []string) string {
	return buildRustFile(crate, imports, funcs)
}

// ToolchainDir es el directorio del package.json más cercano a nombre.
func ToolchainDir(root, nombre string) string { return toolchainDir(root, nombre) }
