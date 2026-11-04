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

func TestNextIDNoReusaUnIDQueYaCorrio(t *testing.T) {
	loom := t.TempDir()
	tasks := filepath.Join(loom, "tasks")
	if err := os.MkdirAll(filepath.Join(loom, "runs", "T-021"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(loom, "runs", "T-021", "attempts.jsonl"), []byte("{}\n"), 0o644)
	_ = os.MkdirAll(filepath.Join(loom, "runs", "_integra"), 0o755)
	// el esqueleto que se está planeando deja su carpeta sin intentos
	_ = os.MkdirAll(filepath.Join(loom, "runs", "T-022"), 0o755)
	_ = os.WriteFile(filepath.Join(loom, "runs", "T-022", "esqueleto-respuesta.json"), []byte("{}"), 0o644)
	id, err := NextID(tasks)
	if err != nil || id != "T-022" {
		t.Fatalf("NextID = %q, %v · T-021 implementó aunque su contrato ya no esté; T-022 solo planeó: quiero T-022", id, err)
	}
}
