// Package wrap turns a local CLI, a cURL/HAR capture or an OpenAPI spec into
// a bridge: a declarative manifest in .loom/bridges/<name>.json whose tools
// the MCP server exposes as bridge_<name>_<tool>.
package wrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Pastranauwu/loom/internal/config"
	"github.com/Pastranauwu/loom/internal/idioma"
)

// Manifest is what the model (or the OpenAPI converter) writes and the
// verify gate checks before it is saved.
type Manifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tools       []Tool `json:"tools"`
}

// Tool is one callable. Exactly one of Exec and HTTP is set. Templates
// carry {var} placeholders that must be required properties of InputSchema.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Exec        *Exec          `json:"exec,omitempty"`
	HTTP        *HTTP          `json:"http,omitempty"`
}

// Exec runs Bin with Args as an argv slice, never through a shell.
type Exec struct {
	Bin  string   `json:"bin"`
	Args []string `json:"args"`
}

type HTTP struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

var (
	reNombre = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)
	reNoNom  = regexp.MustCompile(`[^a-z0-9]+`)
	reVar    = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	reNoVar  = regexp.MustCompile(`[^A-Za-z0-9_]+`)
)

// Slug turns any label into a valid bridge or tool name.
func Slug(s string) string {
	return strings.Trim(reNoNom.ReplaceAllString(strings.ToLower(s), "_"), "_")
}

// ToolName is the name the MCP client sees.
func (m Manifest) ToolName(t Tool) string { return "bridge_" + m.Name + "_" + t.Name }

func Dir(root string) string { return filepath.Join(config.Dir(root), "bridges") }

func Save(root string, m Manifest) error {
	if !reNombre.MatchString(m.Name) {
		return fmt.Errorf(idioma.T("nombre de puente inválido: %q · usa minúsculas, dígitos y _"), m.Name)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(Dir(root), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(Dir(root), m.Name+".json"), append(b, '\n'), 0o644)
}

func Load(root, name string) (Manifest, error) {
	var m Manifest
	if !reNombre.MatchString(name) {
		return m, fmt.Errorf(idioma.T("nombre de puente inválido: %q · usa minúsculas, dígitos y _"), name)
	}
	b, err := os.ReadFile(filepath.Join(Dir(root), name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return m, fmt.Errorf(idioma.T("no existe el puente %s · mira loom wrap list"), name)
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf(idioma.T(".loom/bridges/%s.json no es JSON válido · %w"), name, err)
	}
	m.Name = name
	return m, nil
}

// List reads every manifest. A broken file is reported in bad and skipped,
// so one bad bridge does not hide the rest.
func List(root string) (ok []Manifest, bad []error) {
	files, _ := filepath.Glob(filepath.Join(Dir(root), "*.json"))
	sort.Strings(files)
	for _, f := range files {
		m, err := Load(root, strings.TrimSuffix(filepath.Base(f), ".json"))
		if err != nil {
			bad = append(bad, err)
			continue
		}
		ok = append(ok, m)
	}
	return ok, bad
}

func Remove(root, name string) error {
	if _, err := Load(root, name); err != nil {
		return err
	}
	return os.Remove(filepath.Join(Dir(root), name+".json"))
}
