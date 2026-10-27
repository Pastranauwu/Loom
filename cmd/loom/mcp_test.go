package main

import (
	"strings"
	"testing"
)

func TestBoardDeltaSoloCambios(t *testing.T) {
	dir := t.TempDir()
	uno := `{"pendiente":[{"id":"T-001","titulo":"a"},{"id":"T-002","titulo":"b"}]}`
	if out, _ := boardDelta(dir, uno, false); !strings.Contains(out, "T-001") || !strings.Contains(out, "T-002") {
		t.Fatalf("la primera llamada lista todo: %q", out)
	}
	if out, _ := boardDelta(dir, uno, false); !strings.HasPrefix(out, "no changes") {
		t.Fatalf("sin cambios debe decirlo corto: %q", out)
	}
	dos := `{"listo":[{"id":"T-001","titulo":"a"}],"pendiente":[{"id":"T-002","titulo":"b"}]}`
	out, _ := boardDelta(dir, dos, false)
	if !strings.Contains(out, "T-001 pendiente→listo") || strings.Contains(out, "T-002") {
		t.Fatalf("solo debe salir lo que cambió: %q", out)
	}
}
