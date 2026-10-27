package config

import "strings"

// PlantillaPruebas devuelve el comando de pruebas de un stack conocido
// (go, node/jest, python/pytest). ok es falso si el stack no tiene plantilla:
// no se inventa nada.
func PlantillaPruebas(stack string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(stack)) {
	case "go":
		return "go vet ./... && go test ./...", true
	case "rust", "cargo":
		return "cargo check && cargo clippy -- -D warnings && cargo test", true
	case "typescript", "ts":
		return "npx tsc --noEmit --strict && npx vitest run", true
	case "node", "jest":
		return "npm test", true
	case "python", "pytest":
		return "mypy --strict . && ruff check . && pytest", true
	default:
		return "", false
	}
}

// PlantillasListoCuando devuelve 2–3 ejemplos de listo_cuando para un stack
// conocido. Un stack desconocido devuelve nil: el campo se deja vacío.
func PlantillasListoCuando(stack string) []string {
	switch strings.ToLower(strings.TrimSpace(stack)) {
	case "go":
		return []string{
			"go test ./internal/<paquete>/...",
			"go test ./... -run Test<Nombre>",
			"go test ./cmd/<bin>/...",
		}
	case "node", "jest":
		return []string{
			"npm test -- <archivo>.spec.ts",
			"npx jest src/<modulo>.test.js",
			"npm test -- --testPathPattern=<modulo>",
		}
	case "python", "pytest":
		return []string{
			"pytest tests/test_<modulo>.py",
			"pytest tests/ -k <nombre>",
			"python -m pytest tests/test_<modulo>.py -q",
		}
	default:
		return nil
	}
}

// StackDePlantilla normaliza alias (jest→node, pytest→python) y deja el
// resto igual. Vacío si no hay plantilla.
func StackDePlantilla(stack string) string {
	switch strings.ToLower(strings.TrimSpace(stack)) {
	case "go":
		return "go"
	case "node", "jest":
		return "node"
	case "python", "pytest":
		return "python"
	default:
		return ""
	}
}
