package config

import (
	"encoding/json"
	"errors"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// RepoRoot returns the top-level directory of the git repository
// containing dir.
func RepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New(idioma.T("no hay repositorio git aquí · corre git init y reintenta"))
	}
	return strings.TrimSpace(string(out)), nil
}

// DetectBaseBranch finds the base branch of the repository at root: the
// current branch, else the remote HEAD, else main or master. Empty string
// if nothing can be determined.
//
// La rama actual va primero: es donde el humano está trabajando y donde
// espera el resultado. Con main por delante, en un repo con ramas de
// feature los agentes partían de otro código que el que se veía, y un
// arreglo commiteado en la rama actual nunca les llegaba.
func DetectBaseBranch(root string) string {
	if branch, err := gitOut(root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && branch != "" && !strings.HasPrefix(branch, "loom/") && !strings.HasPrefix(branch, "devclean/") {
		return branch
	}
	if ref, err := gitOut(root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "origin/")
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := gitOut(root, "show-ref", "--verify", "--quiet", "refs/heads/"+candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// DetectTestCommand infers the project test command from well-known
// manifest files, in priority order: package.json with a test script,
// Makefile with a test target, go.mod, pyproject.toml.
func DetectTestCommand(root string) (string, bool) {
	if hasNodeTestScript(root) {
		return "npm test", true
	}
	if hasMakeTestTarget(root) {
		return "make test", true
	}
	if fileExists(filepath.Join(root, "go.mod")) {
		return "go vet ./... && go test ./...", true
	}
	// Django antes que pyproject: su runner arma la base de pruebas
	if fileExists(filepath.Join(root, "manage.py")) {
		return "python manage.py test", true
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) {
		return "pytest", true
	}
	if fileExists(filepath.Join(root, "Cargo.toml")) {
		return "cargo test", true
	}
	return "", false
}

// DetectLanguage infers the project language from well-known manifest
// files. Returns a short label ("go", "node", "python", "rust") or ""
// when nothing is detected.
func DetectLanguage(root string) string {
	switch {
	case fileExists(filepath.Join(root, "go.mod")):
		return "go"
	case fileExists(filepath.Join(root, "package.json")):
		return "node"
	case fileExists(filepath.Join(root, "pyproject.toml")), fileExists(filepath.Join(root, "requirements.txt")):
		return "python"
	case fileExists(filepath.Join(root, "Cargo.toml")):
		return "rust"
	}
	return ""
}

// scaffoldNames are files that exist in a fresh repository and say
// nothing about the code yet.
var scaffoldNames = map[string]bool{
	"README.md":           true,
	"README":              true,
	"LICENSE":             true,
	"LICENSE.md":          true,
	"CHANGELOG.md":        true,
	"CONTRIBUTING.md":     true,
	"CODE_OF_CONDUCT.md":  true,
	".gitignore":          true,
	"loom.spec.yml":       true,
	"loom.spec.yaml":      true,
	"loom.specs.yml":      true,
	"loom.specs.yaml":     true,
	"devclean.spec.yml":   true,
	"devclean.spec.yaml":  true,
	"devclean.specs.yml":  true,
	"devclean.specs.yaml": true,
	"spec.yml":            true,
	"spec.yaml":           true,
	"skills-lock.json":    true, // lo deja loom skills sync, no es código
	".gitattributes":      true,
	".editorconfig":       true,
}

// DetectEmpty reports whether the repository has no source code yet:
// only scaffolding files and hidden directories (.git, .loom,
// .github). Used to switch the planner into greenfield mode.
func DetectEmpty(root string) bool {
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if scaffoldNames[name] {
			continue
		}
		return false
	}
	return true
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasNodeTestScript(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	return strings.TrimSpace(pkg.Scripts["test"]) != ""
}

var makeTestTarget = regexp.MustCompile(`^test\s*:`)

func hasMakeTestTarget(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if makeTestTarget.MatchString(line) {
			return true
		}
	}
	return false
}
