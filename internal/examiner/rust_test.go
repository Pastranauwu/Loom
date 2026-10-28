package examiner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Pastranauwu/loom/internal/loop"
	"github.com/Pastranauwu/loom/internal/task"
)

func TestNombreCrate(t *testing.T) {
	casos := map[string]string{
		"[package]\nname = \"wake-on-lan\"\nversion = \"0.1.0\"\n":                             "wake_on_lan",
		"[package]\nname=\"core\"\n[lib]\nname = \"core_lib\"\n[dependencies]\nname = \"x\"\n": "core_lib",
		"[dependencies]\nserde = \"1\"\n":                                                      "",
	}
	for toml, quiero := range casos {
		if got := nombreCrate(toml); got != quiero {
			t.Errorf("nombreCrate(%q) = %q, quiero %q", toml, got, quiero)
		}
	}
}

func TestCrateYRutasYComandoRust(t *testing.T) {
	if c := crateDeTarea([]string{"crates/red/src/**"}); c != "crates/red" {
		t.Errorf("crate = %q", c)
	}
	if c := crateDeTarea([]string{"src/lib.rs"}); c != "." {
		t.Errorf("crate raíz = %q", c)
	}
	tk := task.Task{ID: "T-007", TocarSolo: []string{"crates/red/src/wol.rs"}}
	v, o := RutasSuite(tk, "rust")
	if v != "crates/red/tests/loom_blind_t_007.rs" || o != "crates/red/tests/loom_blind_t_007_hidden.rs" {
		t.Errorf("rutas = %q %q", v, o)
	}
	if got := strings.Join(ArgsCargo("crates/red", objetivoRust("T-007"), true), " "); got != "test --test loom_blind_t_007 --manifest-path crates/red/Cargo.toml --no-run" {
		t.Errorf("args = %q", got)
	}
	if got := strings.Join(ArgsCargo(".", objetivoRust("T-007"), false), " "); got != "test --test loom_blind_t_007" {
		t.Errorf("args raíz = %q", got)
	}
	if lenguajeDeTarea("", task.Task{TocarSolo: []string{"src/wol.rs"}}) != "rust" {
		t.Error("un .rs en el alcance es rust aunque el repo no diga")
	}
}

func TestExaminableRustPideCargoYBiblioteca(t *testing.T) {
	root := t.TempDir()
	tk := task.Task{ID: "T-001", TocarSolo: []string{"src/**"}, Expone: []string{"pub fn send(mac: Mac) -> Result<(), Error>"}}
	if Examinable(root, tk, "rust") {
		t.Error("sin Cargo.toml no hay crate que importar")
	}
	_ = os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"wol\"\n"), 0o644)
	if Examinable(root, tk, "rust") {
		t.Error("un crate solo binario no se importa desde tests/")
	}
	_ = os.MkdirAll(filepath.Join(root, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "src", "lib.rs"), nil, 0o644)
	if !Examinable(root, tk, "rust") {
		t.Error("con lib.rs se examina")
	}
}

func TestBuildRustFileFiltraImports(t *testing.T) {
	src := buildRustFile("wol", []string{"use std::net::UdpSocket;", "use rand::random;", "use wol::Mac"},
		[]string{"fn manda() {\n    assert!(send(Mac::BROADCAST).is_ok());\n}", "#[test]\n#[should_panic]\nfn vacia() { Mac::parse(\"\"); }"})
	for _, quiero := range []string{"use wol::*;", "use std::net::UdpSocket;", "use wol::Mac;", "#[test]\nfn manda()", "#[test]\n#[should_panic]\nfn vacia()"} {
		if !strings.Contains(src, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, src)
		}
	}
	if strings.Contains(src, "rand") {
		t.Error("un crate externo no entra a la suite")
	}
}

func TestErroresDeSuiteRust(t *testing.T) {
	faltaImpl := "error[E0425]: cannot find function `send` in crate `wol`\n --> tests/loom_blind_t_001.rs:5:9\nerror[E0432]: unresolved import `wol::Mac`\nerror: could not compile `wol` (test \"loom_blind_t_001\") due to 2 previous errors\n"
	if err := erroresDeSuite(faltaImpl, "loom_blind_t_001.rs", "wol"); err != nil {
		t.Errorf("lo que falta implementar no descarta la suite: %v", err)
	}
	sintaxis := "error: expected one of `!` or `::`, found `fn`\n --> tests/loom_blind_t_001.rs:3:1\n"
	if err := erroresDeSuite(sintaxis, "loom_blind_t_001.rs", "wol"); err == nil {
		t.Error("sintaxis rota descarta la suite")
	}
	ajeno := "error[E0433]: failed to resolve: use of undeclared crate or module `rand`\n --> tests/loom_blind_t_001.rs:8:5\n"
	if err := erroresDeSuite(ajeno, "loom_blind_t_001.rs", "wol"); err == nil {
		t.Error("un crate ajeno no compila nunca: descarta")
	}
	if err := erroresDeSuite("error: failed to download `serde`", "loom_blind_t_001.rs", "wol"); err != nil {
		t.Error("si cargo no llegó a la suite, degrada")
	}
}

type agenteRust struct{}

func (agenteRust) Name() string { return "rust" }
func (agenteRust) Run(context.Context, loop.Request) (loop.Result, error) {
	return loop.Result{Text: `{"imports":["use std::net::Ipv4Addr;"],"visible":["#[test]\nfn manda() { assert!(send(\"aa:bb:cc:dd:ee:ff\").is_ok()); }"],"hidden":["#[test]\nfn rechaza() { assert!(send(\"x\").is_err()); }"]}`}, nil
}

func TestRunRustEscribeLaSuiteYCompilaConCargo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cargo falso en shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("sin git")
	}
	room, root := t.TempDir(), t.TempDir()
	exec.Command("git", "-C", room, "init", "-q").Run()
	_ = os.WriteFile(filepath.Join(room, "Cargo.toml"), []byte("[package]\nname = \"wake-on-lan\"\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(room, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(room, "src", "lib.rs"), nil, 0o644)

	bin := t.TempDir()
	registro := filepath.Join(bin, "argv")
	cargo := "#!/bin/sh\necho \"$@\" >> " + registro + "\necho 'error[E0425]: cannot find function `send` in crate `wake_on_lan`'\necho ' --> tests/loom_blind_t_001.rs:4:1'\nexit 101\n"
	if err := os.WriteFile(filepath.Join(bin, "cargo"), []byte(cargo), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	sellada, err := Run(context.Background(), room, Options{
		Agent: agenteRust{}, Root: root, Lenguaje: "rust",
		Task: task.Task{ID: "T-001", Titulo: "wol", TocarSolo: []string{"src/**"}, Expone: []string{"pub fn send(mac: &str) -> Result<(), Error>"}},
	})
	if err != nil || !sellada {
		t.Fatalf("sellada = %v, err = %v", sellada, err)
	}
	b, err := os.ReadFile(filepath.Join(room, "tests", "loom_blind_t_001.rs"))
	if err != nil || !strings.Contains(string(b), "use wake_on_lan::*;") || !strings.Contains(string(b), "fn manda()") {
		t.Fatalf("suite visible = %s, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(room, "tests", "loom_blind_t_001_hidden.rs")); err == nil {
		t.Error("la oculta no se queda en el cuarto")
	}
	llamadas, _ := os.ReadFile(registro)
	if !strings.Contains(string(llamadas), "test --test loom_blind_t_001 --no-run") || !strings.Contains(string(llamadas), "test --test loom_blind_t_001_hidden --no-run") {
		t.Errorf("cargo debe compilar cada suite antes de aceptarla:\n%s", llamadas)
	}
}
