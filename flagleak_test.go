package prompt

import "testing"

// P2: after a failed parse, defaults must be restored on the next run — no leak.
func TestFlagValuesDoNotLeakAfterParseError(t *testing.T) {
	var gotA, gotB int
	h, hErr := testCommand("calc", "", func(a, b int) { gotA, gotB = a, b }, false, Flag("a", "").Default(10), Flag("b", "").Default(20))
	if err := hErr; err != nil {
		t.Fatalf("check: %v", err)
	}

	// run 1: set a=99, but b is invalid -> parse fails
	if err := h.Run("calc -a=99 -b=bad"); err == nil {
		t.Fatal("expected parse error on -b=bad")
	}

	// run 2: no args -> must see the registered defaults, not leaked 99 / partial 0
	if err := h.Run("calc"); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if gotA != 10 || gotB != 20 {
		t.Fatalf("stale flag leak: got a=%d b=%d, want a=10 b=20", gotA, gotB)
	}
}

// P2: after a run that sets values explicitly, the next default run resets them.
func TestFlagValuesResetBetweenRuns(t *testing.T) {
	var gotA int
	h, hErr := testCommand("calc", "", func(a, b int) { gotA = a }, false, Flag("a", "").Default(1), Flag("b", "").Default(2))
	if err := hErr; err != nil {
		t.Fatalf("check: %v", err)
	}
	if err := h.Run("calc -a=777 -b=0"); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if gotA != 777 {
		t.Fatalf("run1 a=%d, want 777", gotA)
	}
	if err := h.Run("calc"); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if gotA != 1 {
		t.Fatalf("run2 a=%d, want default 1 (leaked from run1?)", gotA)
	}
}
