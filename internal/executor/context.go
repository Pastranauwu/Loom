package executor

import "strings"

// ContextWindow is the context size of a model in tokens. The CLIs do not
// report it, so it is a table: 200k, or 1M for ids tagged [1m].
func ContextWindow(model string) int {
	if strings.Contains(model, "[1m]") || strings.HasSuffix(model, "-1m") {
		return 1_000_000
	}
	return 200_000
}
