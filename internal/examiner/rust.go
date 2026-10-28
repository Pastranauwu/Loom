package examiner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Pastranauwu/loom/internal/idioma"
)

// timeoutCargo es aparte del de los parsers: cargo compila las
// dependencias del crate en frío, en un target temporal.
const timeoutCargo = 5 * time.Minute

// crateDeTarea es el directorio del crate, relativo al cuarto: lo que va
// antes de src/ en tocar_solo ("crates/core/src/**" → "crates/core").
func crateDeTarea(tocarSolo []string) string {
	for _, g := range tocarSolo {
		segs := strings.Split(filepath.ToSlash(g), "/")
		for i, s := range segs {
			if s == "src" || s == "Cargo.toml" {
				if i == 0 {
					return "."
				}
				return strings.Join(segs[:i], "/")
			}
		}
	}
	return "."
}

// nombreCrate lee el nombre con que se importa el crate: el de [lib] si lo
// declara, si no el de [package], con '-' cambiado por '_'.
func nombreCrate(cargoToml string) string {
	var seccion, pkg, lib string
	for _, l := range strings.Split(cargoToml, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			seccion = strings.Trim(l, "[] ")
			continue
		}
		clave, valor, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(clave) != "name" {
			continue
		}
		valor = strings.Trim(strings.TrimSpace(valor), `"'`)
		switch seccion {
		case "package":
			pkg = valor
		case "lib":
			lib = valor
		}
	}
	if lib != "" {
		pkg = lib
	}
	return strings.ReplaceAll(pkg, "-", "_")
}

// objetivoRust es el nombre del archivo de pruebas de integración y del
// target de cargo que lo corre: loom_blind_t_001.
func objetivoRust(id string) string {
	return "loom_blind_" + strings.ReplaceAll(strings.ToLower(id), "-", "_")
}

// ArgsCargo arma `cargo test --test <objetivo>` desde la raíz del cuarto.
// noRun solo compila: es la validación previa a escribir la suite.
func ArgsCargo(crate, objetivo string, noRun bool) []string {
	args := []string{"test", "--test", objetivo}
	if crate != "" && crate != "." {
		args = append(args, "--manifest-path", filepath.ToSlash(filepath.Join(crate, "Cargo.toml")))
	}
	if noRun {
		args = append(args, "--no-run")
	}
	return args
}

// crateExaminable lee el nombre del crate de la tarea. Vacío cuando no hay
// examen posible: sin Cargo.toml todavía, o un crate sin biblioteca (una
// prueba de integración no puede importar un binario, como Go con main).
func crateExaminable(root string, tocarSolo []string) (crate, nombre string) {
	crate = crateDeTarea(tocarSolo)
	data, err := os.ReadFile(filepath.Join(root, crate, "Cargo.toml"))
	if err != nil {
		return crate, ""
	}
	nombre = nombreCrate(string(data))
	if nombre == "" {
		return crate, ""
	}
	if _, err := os.Stat(filepath.Join(root, crate, "src", "lib.rs")); err == nil || strings.Contains(string(data), "[lib]") {
		return crate, nombre
	}
	for _, g := range tocarSolo {
		if strings.HasSuffix(g, "lib.rs") {
			return crate, nombre
		}
	}
	return crate, ""
}

// buildRustFile arma la prueba de integración. El crate entra con un glob:
// el examinador llama a la API pública por su nombre. De sus `use` solo
// quedan los de std, core, alloc y el propio crate.
func buildRustFile(crate string, imports, funcs []string) string {
	var b strings.Builder
	b.WriteString("// loom · suite del examinador ciego: criterio de aceptación, no editable.\n")
	b.WriteString("#![allow(unused_imports)]\n\n")
	fmt.Fprintf(&b, "use %s::*;\n", crate)
	for _, imp := range dedup(imports) {
		l := strings.TrimSuffix(strings.TrimSpace(imp), ";")
		l = strings.TrimSpace(strings.TrimPrefix(l, "use "))
		raiz, _, _ := strings.Cut(l, "::")
		if raiz == "std" || raiz == "core" || raiz == "alloc" || raiz == crate {
			fmt.Fprintf(&b, "use %s;\n", l)
		}
	}
	b.WriteString("\n")
	for _, f := range funcs {
		f = strings.TrimSpace(f)
		if !strings.Contains(f, "#[test]") {
			f = "#[test]\n" + f
		}
		b.WriteString(f + "\n\n")
	}
	return b.String()
}

var (
	reErrorRust  = regexp.MustCompile(`^error(\[E(\d+)\])?: (.*)`)
	reCrateAjeno = regexp.MustCompile("undeclared crate or module `([^`]+)`")
)

// faltaImplementacion son los errores de una suite correcta escrita antes
// que el código: símbolos, imports y métodos que todavía no existen.
var faltaImplementacion = map[string]bool{
	"0412": true, "0422": true, "0423": true, "0425": true, "0432": true,
	"0433": true, "0599": true, "0609": true, "0560": true,
}

// validarRust compila la suite con `cargo test --no-run`. Los errores por
// lo que la tarea todavía no implementó se aceptan (es TDD); sintaxis rota
// o un crate ajeno descartan la suite. Sin cargo, o si cargo falla antes
// de llegar a la suite, degrada y deja pasar.
func validarRust(roomPath, rel, contenido string) error {
	if _, err := exec.LookPath("cargo"); err != nil {
		return nil
	}
	abs := filepath.Join(roomPath, filepath.FromSlash(rel))
	crateDir := filepath.Dir(abs)
	for {
		if _, err := os.Stat(filepath.Join(crateDir, "Cargo.toml")); err == nil {
			break
		}
		if crateDir == roomPath || crateDir == filepath.Dir(crateDir) {
			return nil
		}
		crateDir = filepath.Dir(crateDir)
	}
	crateRel, _ := filepath.Rel(roomPath, crateDir)
	if _, err := os.Stat(abs); err != nil {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil
		}
		if err := os.WriteFile(abs, []byte(contenido), 0o644); err != nil {
			return nil
		}
		defer os.Remove(abs)
	}
	target, err := os.MkdirTemp("", "loom-cargo-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(target)

	ctx, cancel := context.WithTimeout(context.Background(), timeoutCargo)
	defer cancel()
	objetivo := strings.TrimSuffix(filepath.Base(abs), ".rs")
	cmd := exec.CommandContext(ctx, "cargo", ArgsCargo(crateRel, objetivo, true)...)
	cmd.Dir = roomPath
	cmd.Env = append(os.Environ(), "CARGO_TARGET_DIR="+target, "CARGO_TERM_COLOR=never")
	salida, err := cmd.CombinedOutput()
	if err == nil || ctx.Err() == context.DeadlineExceeded {
		return nil
	}
	return erroresDeSuite(string(salida), filepath.Base(abs), nombreCrateDe(crateDir))
}

func nombreCrateDe(dir string) string {
	data, _ := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	return nombreCrate(string(data))
}

// erroresDeSuite decide sobre la salida de cargo: nil si solo falta la
// implementación o si cargo no llegó a compilar la suite.
func erroresDeSuite(salida, archivo, crate string) error {
	if !strings.Contains(salida, archivo) {
		return nil
	}
	for _, l := range strings.Split(salida, "\n") {
		m := reErrorRust.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		msg := m[3]
		if strings.HasPrefix(msg, "could not compile") || strings.HasPrefix(msg, "aborting due to") {
			continue
		}
		if c := reCrateAjeno.FindStringSubmatch(msg); c != nil && c[1] != crate {
			return fmt.Errorf(idioma.T("la suite no compila con cargo · %s"), msg)
		}
		if !faltaImplementacion[m[2]] {
			return fmt.Errorf(idioma.T("la suite no compila con cargo · %s"), msg)
		}
	}
	return nil
}
