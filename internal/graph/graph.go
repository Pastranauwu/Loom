// Package graph mantiene el índice del repo en .loom/repo-graph.json:
// símbolos públicos, imports y quién importa a quién, por archivo. Se
// actualiza solo con lo que cambió desde el último commit indexado.
package graph

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const archivo = "repo-graph.json"

type Node struct {
	Hash       string   `json:"hash"`
	Symbols    []string `json:"symbols,omitempty"`
	Imports    []string `json:"imports,omitempty"`
	ImportedBy []string `json:"imported_by,omitempty"`
}

type Graph struct {
	CommitSHA string          `json:"commit_sha"`
	Files     map[string]Node `json:"files"`
}

// Update deja el grafo al día con HEAD y el árbol de trabajo y lo guarda.
// Devuelve cuántos archivos se re-parsearon. En la primera corrida, o si el
// commit guardado ya no existe (rebase), reconstruye todo.
func Update(root, loomDir string) (*Graph, int, error) {
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, 0, err
	}
	head = strings.TrimSpace(head)
	g := Load(loomDir)
	var cand string
	if g == nil || g.CommitSHA == "" || exec.Command("git", "-C", root, "cat-file", "-e", g.CommitSHA+"^{commit}").Run() != nil {
		g = &Graph{Files: map[string]Node{}}
		cand, err = git(root, "ls-files", "-co", "--exclude-standard")
	} else {
		var diff, nuevos string
		if diff, err = git(root, "diff", "--name-only", g.CommitSHA); err == nil {
			nuevos, err = git(root, "ls-files", "-o", "--exclude-standard")
		}
		cand = diff + "\n" + nuevos
	}
	if err != nil {
		return nil, 0, err
	}
	modulo := moduloGo(root)
	reparsed := 0
	for _, f := range strings.Split(cand, "\n") {
		f = strings.TrimSpace(f)
		if f == "" || !indexable(f) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			if _, ok := g.Files[f]; ok {
				delete(g.Files, f)
				reparsed++
			}
			continue
		}
		sum := sha1.Sum(data)
		h := hex.EncodeToString(sum[:])
		if n, ok := g.Files[f]; ok && n.Hash == h {
			continue
		}
		syms, imps := parsear(f, data, modulo)
		g.Files[f] = Node{Hash: h, Symbols: syms, Imports: imps}
		reparsed++
	}
	for f := range g.Files {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			delete(g.Files, f)
			reparsed++
		}
	}
	if reparsed == 0 && g.CommitSHA == head {
		return g, 0, nil
	}
	g.CommitSHA = head
	enlazar(g)
	return g, reparsed, guardar(loomDir, g)
}

func Load(loomDir string) *Graph {
	data, err := os.ReadFile(filepath.Join(loomDir, archivo))
	if err != nil {
		return nil
	}
	var g Graph
	if json.Unmarshal(data, &g) != nil || g.Files == nil {
		return nil
	}
	return &g
}

func guardar(loomDir string, g *Graph) error {
	data, err := json.MarshalIndent(g, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(loomDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(loomDir, archivo), data, 0o644)
}

// enlazar recalcula imported_by sobre todo el grafo: es barato y evita
// aristas viejas cuando un archivo cambia sus imports.
func enlazar(g *Graph) {
	porClave := map[string][]string{}
	for f := range g.Files {
		for _, k := range claves(f) {
			porClave[k] = append(porClave[k], f)
		}
	}
	por := map[string]map[string]bool{}
	for f, n := range g.Files {
		for _, imp := range n.Imports {
			for _, destino := range porClave[imp] {
				if destino == f {
					continue
				}
				if por[destino] == nil {
					por[destino] = map[string]bool{}
				}
				por[destino][f] = true
			}
		}
	}
	for f, n := range g.Files {
		n.ImportedBy = n.ImportedBy[:0]
		for p := range por[f] {
			n.ImportedBy = append(n.ImportedBy, p)
		}
		sort.Strings(n.ImportedBy)
		g.Files[f] = n
	}
}

// claves son los nombres con que otro archivo puede importar a este: el
// directorio en Go, la ruta sin extensión en el resto.
func claves(f string) []string {
	if path.Ext(f) == ".go" {
		return []string{path.Dir(f)}
	}
	stem := strings.TrimSuffix(f, path.Ext(f))
	if b := path.Base(stem); b == "index" || b == "__init__" {
		return []string{stem, path.Dir(f)}
	}
	return []string{stem}
}

func git(root string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	return string(out), err
}

func indexable(f string) bool {
	for _, p := range strings.Split(f, "/") {
		if p == "node_modules" || p == "vendor" || p == "testdata" || strings.HasPrefix(p, ".") {
			return false
		}
	}
	if strings.HasSuffix(f, "_test.go") {
		return false
	}
	switch path.Ext(f) {
	case ".go", ".py", ".js", ".jsx", ".ts", ".tsx", ".mjs":
		return true
	}
	return false
}
