package ship

import (
	"fmt"
	"strings"

	"github.com/Pastranauwu/loom/internal/task"
)

// generarHandoff arma el cuerpo del PR: qué cambió, qué no se
// hizo, riesgos y cómo verificar. Determinista: todo sale del contrato y
// del diff, nada de auto-reportes.
func generarHandoff(t task.Task, archivos []string, mas, menos int) string {
	var b strings.Builder

	b.WriteString("## Qué cambió\n\n")
	if len(archivos) == 0 {
		b.WriteString("- (sin archivos)\n")
	}
	for _, a := range archivos {
		fmt.Fprintf(&b, "- %s\n", a)
	}
	fmt.Fprintf(&b, "\n%d líneas añadidas, %d quitadas.\n", mas, menos)

	b.WriteString("\n## Qué no se hizo\n\n")
	if len(t.NoTocar) > 0 {
		fmt.Fprintf(&b, "- fuera de alcance: %s\n", strings.Join(t.NoTocar, ", "))
	}
	if t.Riesgos != "" {
		fmt.Fprintf(&b, "- riesgos declarados: %s\n", t.Riesgos)
	} else {
		b.WriteString("- sin limitaciones declaradas.\n")
	}

	b.WriteString("\n## Cómo verificar\n\n")
	fmt.Fprintf(&b, "```\n%s\n```\n", t.ListoCuando)

	if t.Porque != "" {
		fmt.Fprintf(&b, "\nMotivo: %s\n", t.Porque)
	}
	return b.String()
}
