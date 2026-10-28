package wrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Pastranauwu/loom/internal/idioma"
	"github.com/Pastranauwu/loom/internal/plan"
)

const formato = `Return ONLY this JSON, no text around it:
{"name": "", "description": "", "tools": [{"name": "", "description": "", "inputSchema": {"type": "object", "properties": {"x": {"type": "string", "description": ""}}, "required": ["x"]}, "exec": {"bin": "", "args": [""]}}]}

Rules:
- "name" and every tool "name": lowercase letters, digits and underscores.
- inputSchema is standard JSON Schema with "type": "object".
- Templates use {var} placeholders. EVERY {var} must be a property of inputSchema AND listed in "required"; if an option may be absent, make a separate tool for it.
- A tool has either "exec" or "http", never both.
- "exec": "bin" is the bare binary name; "args" is a list, one entry per argv element ("--out", "{path}" are two entries). Never use sh, bash or any shell, and never join arguments into one string. A value that opens its own argv entry is rejected at call time if it starts with "-"; prefer "--flag={var}", or set "allow_leading_dash": true on the property only when such values are legitimate (negative numbers).
- "http": {"method": "GET", "url": "https://...", "headers": {"Name": "value"}, "body": "{\"k\": \"{v}\"}"}. Keep header placeholders like {authorization} for secrets and make them required string properties.
- Only describe what the input below shows; do not invent flags or endpoints.`

// Prompt asks the planner for a manifest. modo is "cli" or "api"; contexto
// is the help text or the request summary.
func Prompt(modo, nombre, contexto string) string {
	var b strings.Builder
	b.WriteString("You are Loom Wrap. Turn the input below into a bridge manifest: a small set of typed tools an AI agent can call.\n\n")
	if nombre != "" {
		b.WriteString("Bridge name: " + nombre + "\n\n")
	}
	if modo == "cli" {
		b.WriteString("INPUT: help text of a command-line program. Make one \"exec\" tool per useful read or query subcommand (at most 12); skip interactive, destructive and installation subcommands.\n```\n" + contexto + "\n```\n\n")
	} else {
		b.WriteString("INPUT: captured HTTP calls of an API. Make one \"http\" tool per distinct call, turning values that change between calls into {var} placeholders.\n```json\n" + contexto + "\n```\n\n")
	}
	b.WriteString(formato)
	return b.String()
}

// Parse reads the planner's manifest. The verify gate runs after it.
func Parse(texto, nombre string) (Manifest, error) {
	t := plan.CuerpoJSON(texto)
	if t == "" {
		return Manifest{}, errors.New(idioma.T("el modelo no devolvió un manifiesto · vuelve a intentarlo"))
	}
	var m Manifest
	if err := json.NewDecoder(strings.NewReader(plan.EscaparControles(t))).Decode(&m); err != nil {
		return m, fmt.Errorf(idioma.T("el manifiesto no es JSON válido · %s"), err)
	}
	if nombre != "" {
		m.Name = Slug(nombre)
	} else {
		m.Name = Slug(m.Name)
	}
	return m, nil
}
