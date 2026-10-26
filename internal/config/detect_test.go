package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestRepoRootOutsideGit(t *testing.T) {
	_, err := RepoRoot(t.TempDir())
	if err == nil {
		t.Fatal("RepoRoot debió fallar fuera de un repo git")
	}
}

func TestRepoRootFindsTopLevel(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := RepoRoot(sub)
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	// git puede resolver symlinks (p. ej. /tmp en macOS); compara resueltos
	want, _ := filepath.EvalSymlinks(root)
	gotEval, _ := filepath.EvalSymlinks(got)
	if gotEval != want {
		t.Errorf("RepoRoot = %q, quiero %q", got, root)
	}
}

func TestDetectBaseBranchUnbornHead(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "main")
	if got := DetectBaseBranch(root); got != "main" {
		t.Errorf("DetectBaseBranch = %q, quiero %q", got, "main")
	}
}

// La rama en la que está el humano gana sobre main: ahí espera el
// resultado. En una rama de loom o sin rama, cae a main.
func TestDetectBaseBranchPrefiereLaRamaActual(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "dev")
	git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	git(t, root, "branch", "main")
	git(t, root, "branch", "master")
	if got := DetectBaseBranch(root); got != "dev" {
		t.Errorf("DetectBaseBranch = %q, quiero %q", got, "dev")
	}
	git(t, root, "checkout", "-q", "-b", "loom/T-001")
	if got := DetectBaseBranch(root); got != "main" {
		t.Errorf("en una rama de loom: DetectBaseBranch = %q, quiero %q", got, "main")
	}
	git(t, root, "checkout", "-q", "--detach")
	if got := DetectBaseBranch(root); got != "main" {
		t.Errorf("sin rama: DetectBaseBranch = %q, quiero %q", got, "main")
	}
}

func TestDetectBaseBranchFallsBackToCurrent(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "-b", "desarrollo")
	git(t, root, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	if got := DetectBaseBranch(root); got != "desarrollo" {
		t.Errorf("DetectBaseBranch = %q, quiero %q", got, "desarrollo")
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "go.mod", files: map[string]string{"go.mod": "module x\n"}, want: "go"},
		{name: "package.json", files: map[string]string{"package.json": "{}"}, want: "node"},
		{name: "pyproject", files: map[string]string{"pyproject.toml": "[project]\n"}, want: "python"},
		{name: "cargo", files: map[string]string{"Cargo.toml": "[package]\n"}, want: "rust"},
		{name: "nada", files: map[string]string{"README.md": "hola\n"}, want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := DetectLanguage(root); got != tc.want {
				t.Errorf("DetectLanguage = %q, quiero %q", got, tc.want)
			}
		})
	}
}

func TestDetectEmpty(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		dirs  []string
		want  bool
	}{
		{name: "solo README", files: map[string]string{"README.md": "hola\n"}, want: true},
		{name: "README y LICENSE", files: map[string]string{"README.md": "h", "LICENSE": "MIT"}, want: true},
		{name: "con código", files: map[string]string{"README.md": "h", "main.go": "package main\n"}, want: false},
		{name: "con carpeta de código", dirs: []string{"src"}, want: false},
		{name: "con hidden", dirs: []string{".github"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range tc.dirs {
				if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if got := DetectEmpty(root); got != tc.want {
				t.Errorf("DetectEmpty = %v, quiero %v", got, tc.want)
			}
		})
	}
}

func TestDetectTestCommand(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  string
		ok    bool
	}{
		{
			name:  "package.json con script test",
			files: map[string]string{"package.json": `{"scripts":{"test":"vitest"}}`},
			want:  "npm test",
			ok:    true,
		},
		{
			name: "package.json sin test cae a Makefile",
			files: map[string]string{
				"package.json": `{"scripts":{"build":"tsc"}}`,
				"Makefile":     "build:\n\techo build\n\ntest:\n\techo test\n",
			},
			want: "make test",
			ok:   true,
		},
		{
			name:  "go.mod",
			files: map[string]string{"go.mod": "module x\n\ngo 1.22\n"},
			want:  "go test ./...",
			ok:    true,
		},
		{
			name:  "pyproject.toml",
			files: map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n"},
			want:  "pytest",
			ok:    true,
		},
		{
			name: "Makefile sin target test no detecta",
			files: map[string]string{
				"Makefile": "build:\n\techo build\n",
			},
			want: "",
			ok:   false,
		},
		{
			name:  "sin manifiestos",
			files: map[string]string{"README.md": "hola\n"},
			want:  "",
			ok:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := DetectTestCommand(root)
			if got != tc.want || ok != tc.ok {
				t.Errorf("DetectTestCommand = (%q, %v), quiero (%q, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestSpecNoCuentaComoCodigo(t *testing.T) {
	for _, nombre := range []string{"loom.spec.yml", "loom.specs.yml", "loom.specs.yaml"} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, nombre), []byte("feature: snake\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if !DetectEmpty(root) {
			t.Fatalf("%s desactivó el arranque desde cero", nombre)
		}
	}
}
