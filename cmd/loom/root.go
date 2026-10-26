package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Pastranauwu/loom/internal/ui"
)

// out is the shared printer for all commands, configured in the root
// PersistentPreRun from --plain/--json and the nature of stdout.
var out *ui.Printer

// plainMode recuerda si se pidió --plain o si stdout no es terminal. Los
// comandos lo usan para decidir entre TUI y texto plano.
var plainMode bool

func newRootCmd() *cobra.Command {
	var jsonOut bool
	var plain bool

	root := &cobra.Command{
		Use:           "loom",
		Short:         "tú diseñas el patrón, loom teje el código con agentes de IA",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if !jsonOut && !plain && !isTerminal(os.Stdout) {
				plain = true
			}
			plainMode = plain
			out = ui.New(os.Stdout, jsonOut)
		},
	}

	root.PersistentFlags().BoolVar(&plain, "plain", false, "salida en texto plano, una línea por evento")
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "salida estructurada en JSON")

	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if en, ok := aliasBanderas[name]; ok {
			name = en
		}
		return pflag.NormalizedName(name)
	})

	root.AddCommand(
		newInitCmd(),
		newPlanCmd(),
		newApplyCmd(),
		newUpCmd(),
		newPsCmd(),
		newTaskCmd(),
		newCheckCmd(),
		newRunCmd(),
		newStopCmd(),
		newShipCmd(),
		newBoardCmd(),
		newLogsCmd(),
		newReportCmd(),
		newDoctorCmd(),
		newConstitutionCmd(),
		newStandupCmd(),
		newSkillsCmd(),
		newUsageCmd(),
		newModelosCmd(),
		newArchiveCmd(),
		newRepararCmd(),
		newLimpiarCmd(),
		newGraphCmd(),
		newDNACmd(),
		newSketchCmd(),
		newHealCmd(),
		newMapCmd(),
	)

	return root
}

// esTUI reporta si el comando debe correr en modo interactivo: salida a
// terminal, sin --plain ni --json.
func esTUI() bool {
	return !out.JSON() && !plainMode && isTerminal(os.Stdout)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// aliasBanderas son los nombres en español de las banderas, que siguen
// funcionando aunque la ayuda muestre solo el nombre en inglés.
var aliasBanderas = map[string]string{
	"agentes":    "agents",
	"revisar":    "review",
	"integrar":   "merge",
	"fondo":      "background",
	"reintentar": "retry",
	"todas":      "all",
}
