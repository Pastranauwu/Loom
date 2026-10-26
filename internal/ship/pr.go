package ship

import (
	"context"
	"errors"
	"fmt"
	"github.com/Pastranauwu/loom/internal/idioma"
	"os"
	"os/exec"
	"strings"

	"github.com/Pastranauwu/loom/internal/room"
)

// abrirPR sube la rama, abre el PR con gh y libera el cuarto.
// Devuelve la URL del PR. gh es la vía de v0.1; el fallback por API REST
// llega después.
func abrirPR(ctx context.Context, root string, r room.Room, base, titulo, cuerpo string) (string, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return "", errors.New(idioma.T("gh no está instalado · instálalo o usa --dry-run"))
	}

	// sin remoto el push falla con un "exit status 128" que no dice nada;
	// un repo local es un caso normal, no un error del usuario
	if _, err := gitRun(root, "remote", "get-url", "origin"); err != nil {
		return "", errors.New(idioma.T("sin remoto origin · agrégalo con git remote add origin <url> o usa --dry-run"))
	}

	// --force: la rama loom/<id> es propiedad exclusiva de este cuarto.
	// Cada `ship` aplana de nuevo sobre la base (aplanar en git.go), así
	// que un reintento tras un fallo parcial (p. ej. `gh pr create` caído
	// por red después de que el push ya había salido) produce un commit
	// hermano del que ya está en origin: mismo padre, hash distinto. Sin
	// --force ese push se rechaza por non-fast-forward y el reintento
	// nunca llega a abrir el PR. Nadie más empuja a esta rama.
	if out, err := gitRun(r.Path, "push", "--force", "-u", "origin", r.Rama); err != nil {
		return "", fmt.Errorf(idioma.T("no se pudo subir la rama · %s"), tail(out))
	}

	// si un intento previo ya empujó y abrió el PR pero loom se cortó
	// antes de reportarlo (o `gh pr create` fue el que falló después de
	// crearlo, cosa que gh reporta con error igual en algunas versiones),
	// un reintento no debe duplicar el PR: lo detecta y lo devuelve tal cual.
	if url := prExistente(ctx, root, gh, r.Rama); url != "" {
		_ = room.Destroy(ctx, root, r.ID)
		return url, nil
	}

	f, err := os.CreateTemp("", "loom-handoff-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(cuerpo); err != nil {
		f.Close()
		return "", err
	}
	f.Close()

	cmd := exec.CommandContext(ctx, gh, "pr", "create", "--base", base, "--head", r.Rama, "--title", titulo, "--body-file", f.Name())
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		// gh a veces crea el PR y responde con error igual (timeout tras
		// crear, rate limit en la respuesta): revisa antes de reportar
		// fallo, para no bloquear un PR que en realidad ya existe.
		if url := prExistente(ctx, root, gh, r.Rama); url != "" {
			_ = room.Destroy(ctx, root, r.ID)
			return url, nil
		}
		return "", fmt.Errorf(idioma.T("gh no pudo abrir el PR · %s"), tail(string(out)))
	}

	// liberar el cuarto; si falla, el PR ya está abierto, no se rompe
	_ = room.Destroy(ctx, root, r.ID)

	return strings.TrimSpace(string(out)), nil
}

// prExistente devuelve la URL del PR abierto para esta rama, si ya existe.
// Cadena vacía si no hay ninguno o gh no puede confirmarlo.
func prExistente(ctx context.Context, root, gh, rama string) string {
	cmd := exec.CommandContext(ctx, gh, "pr", "view", rama, "--json", "url", "-q", ".url")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
