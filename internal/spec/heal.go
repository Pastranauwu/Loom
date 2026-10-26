package spec

import (
	"bytes"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"slices"

	"gopkg.in/yaml.v3"
)

// AgregarReglaYCriterio appends a rule and an acceptance criterion to the
// spec file at path, keeping its comments and everything else as written.
// The result is parsed again before touching the file; on any problem the
// file stays as it was.
func AgregarReglaYCriterio(path, regla string, a Acceptance) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf(idioma.T("%s no es un mapa YAML"), path)
	}
	m := doc.Content[0]
	agregarItem(m, []string{"rules", "reglas"}, "rules", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: regla})
	item := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: a.Criterion}
	if a.Command != "" {
		item = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "criterion"}, {Kind: yaml.ScalarNode, Tag: "!!str", Value: a.Criterion},
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "command"}, {Kind: yaml.ScalarNode, Tag: "!!str", Value: a.Command},
		}}
	}
	agregarItem(m, []string{"acceptance", "aceptacion"}, "acceptance", item)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if _, err := Parse(buf.Bytes()); err != nil {
		return fmt.Errorf(idioma.T("el spec editado no parsea, no se escribió: %w"), err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// agregarItem adds item to the sequence under any of claves; with nested
// categories it goes under a "heal" category, and a missing key is created.
func agregarItem(m *yaml.Node, claves []string, nueva string, item *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !slices.Contains(claves, m.Content[i].Value) {
			continue
		}
		v := m.Content[i+1]
		switch v.Kind {
		case yaml.SequenceNode:
			v.Content = append(v.Content, item)
		case yaml.MappingNode:
			for j := 0; j+1 < len(v.Content); j += 2 {
				if v.Content[j].Value == "heal" && v.Content[j+1].Kind == yaml.SequenceNode {
					v.Content[j+1].Content = append(v.Content[j+1].Content, item)
					return
				}
			}
			v.Content = append(v.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "heal"},
				&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{item}})
		default: // `rules:` with nothing under it
			*v = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{item}}
		}
		return
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: nueva},
		&yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{item}})
}
