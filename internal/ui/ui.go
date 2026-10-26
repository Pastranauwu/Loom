// Package ui implements the two output modes of loom:
// plain text (one line per event) and structured JSON.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
)

// Printer writes command output in plain or JSON mode.
// Plain is the default and the only visual style of the task core:
// lowercase lines, no trailing period, no colors.
type Printer struct {
	w    io.Writer
	json bool
}

// New returns a Printer that writes to w.
func New(w io.Writer, jsonMode bool) *Printer {
	return &Printer{w: w, json: jsonMode}
}

// JSON reports whether structured output is active.
func (p *Printer) JSON() bool { return p.json }

// Line writes one line of text in plain mode. In JSON mode it is a
// no-op: structured output goes through Data.
func (p *Printer) Line(format string, args ...any) {
	if p.json {
		return
	}
	fmt.Fprintf(p.w, format+"\n", args...)
}

// Data writes v as indented JSON in JSON mode, nothing otherwise.
func (p *Printer) Data(v any) error {
	if !p.json {
		return nil
	}
	enc := json.NewEncoder(p.w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
