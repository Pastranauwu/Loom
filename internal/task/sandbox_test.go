package task

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// reiniciarSonda deja que la próxima llamada vuelva a buscar bwrap.
func reiniciarSonda(t *testing.T) {
	t.Helper()
	sondaOnce, bwrapRuta, estado = sync.Once{}, "", "apagado"
	t.Cleanup(func() { sondaOnce, bwrapRuta, estado = sync.Once{}, "", "apagado" })
}

func TestArgsBwrap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".cache", ".cargo"} {
		os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "T-001"), 0o755)
	cuarto := t.TempDir()
	os.WriteFile(filepath.Join(cuarto, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "T-001")+"\n"), 0o644)

	args := strings.Join(ArgsBwrap(cuarto, false), " ")
	for _, quiero := range []string{
		"--die-with-parent --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp",
		"--bind " + filepath.Join(home, ".cache") + " " + filepath.Join(home, ".cache"),
		"--bind " + filepath.Join(home, ".cargo") + " " + filepath.Join(home, ".cargo"),
		"--bind " + filepath.Join(repo, ".git") + " " + filepath.Join(repo, ".git"),
		"--bind " + cuarto + " " + cuarto,
		"--chdir " + cuarto,
		"--unshare-net",
	} {
		if !strings.Contains(args, quiero) {
			t.Errorf("falta %q en:\n%s", quiero, args)
		}
	}
	if strings.Contains(args, ".npm") {
		t.Error("una caché que no existe no se monta")
	}
	if strings.Index(args, "--tmpfs /tmp") > strings.Index(args, "--bind "+cuarto) {
		t.Error("el cuarto se monta después de /tmp: si vive en /tmp, el tmpfs lo taparía")
	}
	herramientas := t.TempDir()
	t.Setenv("PATH", herramientas+string(os.PathListSeparator)+"/usr/bin")
	if a := strings.Join(ArgsBwrap(cuarto, false), " "); !strings.Contains(a, "--ro-bind "+herramientas+" "+herramientas) {
		t.Errorf("el PATH bajo /tmp se monta en solo lectura:\n%s", a)
	}
	if strings.Contains(strings.Join(ArgsBwrap(cuarto, true), " "), "--unshare-net") {
		t.Error("una tarea con red no pierde la red")
	}
}

func TestShellSinBwrapCaeANativoConAviso(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	reiniciarSonda(t)
	sh, _ := exec.LookPath("sh")
	vacio := t.TempDir()
	os.Symlink(sh, filepath.Join(vacio, "sh"))
	t.Setenv("PATH", vacio)
	var avisos []string
	prev := Aviso
	Aviso = func(s string) { avisos = append(avisos, s) }
	defer func() { Aviso = prev }()

	dir := t.TempDir()
	c := Shell(context.Background(), dir, "echo hola", false)
	if filepath.Base(c.Path) != "sh" || c.Dir != dir {
		t.Fatalf("sin bwrap corre nativo en el cuarto: %v en %s", c.Args, c.Dir)
	}
	if out, err := c.CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "hola" {
		t.Fatalf("salida %q %v", out, err)
	}
	Shell(context.Background(), dir, "true", false)
	if len(avisos) != 1 || EstadoSandbox() != "ausente" {
		t.Errorf("un aviso por proceso: %v · estado %s", avisos, EstadoSandbox())
	}
}

func TestShellConBwrapBloqueadoCaeANativo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	reiniciarSonda(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\nexit 1\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	prev := Aviso
	Aviso = func(string) {}
	defer func() { Aviso = prev }()
	c := Shell(context.Background(), t.TempDir(), "true", false)
	if filepath.Base(c.Path) != "sh" || EstadoSandbox() != "bloqueado" {
		t.Errorf("bwrap que no arranca: %v · %s", c.Args, EstadoSandbox())
	}
}

func TestShellConBwrapYApagado(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	reiniciarSonda(t)
	bin := t.TempDir()
	os.WriteFile(filepath.Join(bin, "bwrap"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	c := Shell(context.Background(), dir, "go test ./...", false)
	args := strings.Join(c.Args, " ")
	if filepath.Base(c.Path) != "bwrap" || !strings.Contains(args, "--unshare-net -- sh -c go test ./...") || EstadoSandbox() != "activo" {
		t.Errorf("con bwrap el comando va dentro: %s · %s", args, EstadoSandbox())
	}
	Sandbox = "off"
	defer func() { Sandbox = "" }()
	if c := Shell(context.Background(), dir, "true", false); filepath.Base(c.Path) != "sh" {
		t.Errorf("sandbox: off corre nativo: %v", c.Args)
	}
}

// Con un bwrap real: el cuarto se escribe, fuera de él no, y sin red no
// hay más que loopback.
func TestSandboxRealAislaElCuarto(t *testing.T) {
	reiniciarSonda(t)
	prev := Aviso
	Aviso = func(string) {}
	defer func() { Aviso = prev }()
	if EstadoSandbox() != "activo" {
		t.Skip("sin bwrap utilizable")
	}
	cuarto, afuera := t.TempDir(), t.TempDir()
	if out, err := Shell(context.Background(), cuarto, "echo ok > dentro.txt", false).CombinedOutput(); err != nil {
		t.Fatalf("el cuarto debe poder escribirse: %v %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(cuarto, "dentro.txt")); err != nil {
		t.Error("lo escrito en el cuarto queda en el cuarto")
	}
	Shell(context.Background(), cuarto, "echo x > "+filepath.Join(afuera, "fuga.txt"), false).Run()
	if _, err := os.Stat(filepath.Join(afuera, "fuga.txt")); err == nil {
		t.Error("fuera del cuarto no se escribe")
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := Shell(context.Background(), cuarto, "touch "+filepath.Join(home, ".loom-sandbox-prueba"), false).Run(); err == nil {
			os.Remove(filepath.Join(home, ".loom-sandbox-prueba"))
			t.Error("el home es de solo lectura")
		}
	}
	if out, _ := Shell(context.Background(), cuarto, "cat /proc/net/dev | grep -c :", false).Output(); strings.TrimSpace(string(out)) != "1" {
		t.Errorf("sin red solo queda loopback: %s interfaces", out)
	}
}
