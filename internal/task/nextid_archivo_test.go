package task

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextIDCuentaLoArchivado(t *testing.T) {
	loom := t.TempDir()
	tasks := filepath.Join(loom, "tasks")
	viejo := filepath.Join(loom, "archivo", "v1", "tasks")
	if err := os.MkdirAll(viejo, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(viejo, "T-009.md"), nil, 0o644)
	id, err := NextID(tasks)
	if err != nil || id != "T-010" {
		t.Fatalf("NextID = %q, %v · quiero T-010", id, err)
	}
}
