package loop

import "testing"

func TestParseTestCountsPytest(t *testing.T) {
	p, f := ParseTestCounts("5 passed, 4 failed in 2.3s")
	if p == nil || f == nil || *p != 5 || *f != 4 {
		t.Fatalf("pytest = %v/%v, quiero 5/4", p, f)
	}
}

func TestParseTestCountsJest(t *testing.T) {
	p, f := ParseTestCounts("Tests:       5 passed, 4 failed, 9 total")
	if p == nil || f == nil || *p != 5 || *f != 4 {
		t.Fatalf("jest = %v/%v, quiero 5/4", p, f)
	}
}

func TestParseTestCountsMocha(t *testing.T) {
	p, f := ParseTestCounts("  5 passing (12ms)\n  2 failing\n")
	if p == nil || f == nil || *p != 5 || *f != 2 {
		t.Fatalf("mocha = %v/%v, quiero 5/2", p, f)
	}
	// solo passing: el fallido se omite cuando es cero
	p, f = ParseTestCounts("  3 passing (1ms)\n")
	if p == nil || f == nil || *p != 3 || *f != 0 {
		t.Fatalf("mocha passing solo = %v/%v, quiero 3/0", p, f)
	}
}

func TestParseTestCountsSinFormato(t *testing.T) {
	// go test no imprime contadores: null, no un número inventado
	if p, f := ParseTestCounts("ok  pkg/foo  0.123s\n--- FAIL: TestBar\nFAIL"); p != nil || f != nil {
		t.Fatalf("salida sin contadores = %v/%v, quiero nil/nil", p, f)
	}
	if p, f := ParseTestCounts(""); p != nil || f != nil {
		t.Fatalf("salida vacía = %v/%v, quiero nil/nil", p, f)
	}
}

func TestSinPruebas(t *testing.T) {
	casos := []struct {
		nombre string
		salida string
		quiero bool
	}{
		{"go paquete sin pruebas", "?   \tsandbox/numeros\t[no test files]\n", true},
		{"go sin pruebas que correr", "testing: warning: no tests to run\nPASS\nok  \tsandbox/x\t0.001s\n", false},
		{"go multipaquete, otros verdes", "ok  \tsandbox/cola\t0.004s\n?   \tsandbox/numeros\t[no test files]\nok  \tsandbox/texto\t0.003s\n", false},
		{"go verde normal", "ok  \tsandbox/cola\t0.004s\n", false},
		{"go rojo", "--- FAIL: TestX\nFAIL\tsandbox/cola\t0.01s\n", false},
		{"pytest sin recolectar", "collected 0 items\n\nno tests ran in 0.01s\n", true},
		{"pytest verde", "collected 3 items\n\n3 passed in 0.10s\n", false},
		{"jest sin pruebas", "No tests found, exiting with code 0\n", true},
		{"salida vacía", "", false},
	}
	for _, c := range casos {
		if got := SinPruebas(c.salida); got != c.quiero {
			t.Errorf("%s: SinPruebas = %v, quiero %v", c.nombre, got, c.quiero)
		}
	}
}

func TestCargoSumaTodosLosBinariosYCazaElVerdeVacio(t *testing.T) {
	vacio := "running 0 tests\n\ntest result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 3 filtered out; finished in 0.00s\n\n   Doc-tests x\n\nrunning 0 tests\n\ntest result: ok. 0 passed; 0 failed; 0 ignored; 0 measured; 0 filtered out; finished in 0.00s\n"
	if !SinPruebas(vacio) {
		t.Error("cargo con filtro que no encuentra nada salió verde")
	}
	mixto := "running 0 tests\n\ntest result: ok. 0 passed; 0 failed; 0 ignored\n\nrunning 2 tests\ntest a ... ok\n\ntest result: ok. 2 passed; 0 failed; 0 ignored\n"
	if SinPruebas(mixto) {
		t.Error("corrieron 2 pruebas en el segundo binario y se leyó como vacío")
	}
	if p, f := ParseTestCounts(mixto); p == nil || *p != 2 || *f != 0 {
		t.Errorf("conteo = %v %v, quiero 2 0", p, f)
	}
}
