package executor

import "strings"

// ContextWindow is the context size of a model in tokens. The CLIs do not
// report it, so it is a table: 200k, or 1M for ids tagged [1m].
func ContextWindow(model string) int {
	m := strings.ToLower(model)
	if strings.Contains(m, "[1m]") || strings.HasSuffix(m, "-1m") || strings.Contains(m, "gemini") {
		return 1_000_000
	}
	return 200_000
}
