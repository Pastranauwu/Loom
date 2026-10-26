// Package sketch turns a one-line idea into a spec: a model interrogates the
// idea like an aggressive product manager, the human answers, and a second
// call writes the spec from the answers.
package sketch

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/plan"
	"github.com/Pastranauwu/loom/internal/spec"
)

const NumPreguntas = 4

type Pregunta struct {
	Pregunta string `json:"question"`
	Porque   string `json:"why"`
}

type Respuesta struct{ Pregunta, Respuesta string }

// PromptPreguntas asks the model for the questions that expose what the idea leaves out.
func PromptPreguntas(idea, repo string) string {
	var b strings.Builder
	b.WriteString("You are Loom Sketch, an aggressive product manager. A human gave you a one-line idea for software. Your job is to find what they have not thought of before any code is written.\n\n")
	fmt.Fprintf(&b, "IDEA\n%s\n\n", idea)
	if repo != "" {
		fmt.Fprintf(&b, "REPOSITORY\n%s\nDo not ask what the repository already answers.\n\n", repo)
	}
	fmt.Fprintf(&b, "Ask exactly %d questions. Each one targets an edge case, a missing rule, a failure mode or a scope boundary whose wrong default would break the feature or surprise the user. One sentence per question, answerable in one line. No pleasantries, no questions that only restate the idea. Prefer questions that force a choice.\n\n", NumPreguntas)
	b.WriteString("Return ONLY this JSON, no text around it:\n")
	b.WriteString(`{"questions": [{"question": "the question", "why": "what breaks if it is answered wrong"}]}`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// ParsePreguntas reads the model's questions; at least one, at most NumPreguntas.
func ParsePreguntas(texto string) ([]Pregunta, error) {
	var doc struct {
		Questions []Pregunta `json:"questions"`
	}
	if err := decodificar(texto, &doc); err != nil {
		return nil, err
	}
	var out []Pregunta
	for _, q := range doc.Questions {
		if strings.TrimSpace(q.Pregunta) != "" {
			out = append(out, q)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("el modelo no hizo ninguna pregunta · vuelve a intentarlo")
	}
	if len(out) > NumPreguntas {
		out = out[:NumPreguntas]
	}
	return out, nil
}

// PromptSpec asks for the spec that the idea and the answers imply.
func PromptSpec(idea string, qa []Respuesta, repo string) string {
	var b strings.Builder
	b.WriteString("You are Loom Sketch. Turn this idea and the human's answers into a software spec.\n\n")
	fmt.Fprintf(&b, "IDEA\n%s\n\nINTERVIEW\n", idea)
	for _, r := range qa {
		a := strings.TrimSpace(r.Respuesta)
		if a == "" {
			a = "(no answer: choose the conservative option and state it as a rule starting with \"Assumption:\")"
		}
		fmt.Fprintf(&b, "Q: %s\nA: %s\n", r.Pregunta, a)
	}
	if repo != "" {
		fmt.Fprintf(&b, "\nREPOSITORY\n%s\n", repo)
	}
	b.WriteString(`
Rules for the spec:
- "feature": a short lowercase title.
- "why": one or two lines on why it is wanted.
- "requirements": atomic, testable statements of behavior, one behavior each. Cover the edge cases the answers settled.
- "rules": constraints on how it is built that apply to everything, including every "Assumption:".
- "acceptance": end-to-end criteria. Give a "command" only if it is an executable command that certainly exists in this repository (for example the project's test command); otherwise leave it "".
- "dont_touch": globs of paths that must not change, only if the answers name them; otherwise [].

Return ONLY this JSON, no text around it:
{"feature": "", "why": "", "requirements": [""], "rules": [""], "acceptance": [{"criterion": "", "command": ""}], "dont_touch": []}`)
	b.WriteString(idioma.Instruccion())
	return b.String()
}

// ParseSpec builds the spec from the model's answer.
func ParseSpec(texto string) (spec.Spec, error) {
	var doc struct {
		Feature      string            `json:"feature"`
		Why          string            `json:"why"`
		Requirements []string          `json:"requirements"`
		Rules        []string          `json:"rules"`
		Acceptance   []json.RawMessage `json:"acceptance"`
		DontTouch    []string          `json:"dont_touch"`
	}
	if err := decodificar(texto, &doc); err != nil {
		return spec.Spec{}, err
	}
	s := spec.Spec{Version: 1, Feature: strings.TrimSpace(doc.Feature), Motivo: strings.TrimSpace(doc.Why)}
	for _, r := range doc.Requirements {
		if r = strings.TrimSpace(r); r != "" {
			s.Requirements = append(s.Requirements, r)
		}
	}
	for _, r := range doc.Rules {
		if r = strings.TrimSpace(r); r != "" {
			s.Reglas = append(s.Reglas, r)
		}
	}
	for _, raw := range doc.Acceptance {
		var a spec.Acceptance
		if json.Unmarshal(raw, &a.Criterion) != nil { // a bare string is a textual criterion
			var obj struct{ Criterion, Command string }
			if json.Unmarshal(raw, &obj) != nil {
				continue
			}
			a = spec.Acceptance{Criterion: obj.Criterion, Command: obj.Command}
		}
		if a.Criterion = strings.TrimSpace(a.Criterion); a.Criterion != "" {
			a.Command = strings.TrimSpace(a.Command)
			s.Acceptance = append(s.Acceptance, a)
		}
	}
	s.Constraints.NoTocar = doc.DontTouch
	if s.Feature == "" || len(s.Requirements) == 0 {
		return spec.Spec{}, errors.New("el modelo no devolvió un spec con feature y requirements · vuelve a intentarlo")
	}
	return s, nil
}

func decodificar(texto string, v any) error {
	t := plan.CuerpoJSON(texto)
	if t == "" {
		return errors.New("el modelo no devolvió JSON · vuelve a intentarlo")
	}
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(t))).Decode(v); err != nil {
		return fmt.Errorf("el modelo devolvió JSON inválido · %s", err)
	}
	return nil
}
