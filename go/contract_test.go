package revera

import "testing"

func TestContractOmitsUnreachableCaptureBackends(t *testing.T) {
	re, err := Compile("a*", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile failed: %v", err)
	}
	c := ContractFor(&re, 64)
	if c.HasOnePass || c.HasSolver {
		t.Fatalf("zero-group contract reports phase B: %+v", c)
	}
	if ContractHeapBytes(&c) != c.Matcher.HeapBytes ||
		ContractStackBytes(&c) != c.Matcher.StackBytes ||
		ContractSteps(&c) != c.Matcher.Steps {
		t.Fatalf("zero-group contract is not matcher-only: %+v", c)
	}

	grouped, err := Compile("(a*)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile grouped expression failed: %v", err)
	}
	groupedContract := ContractFor(&grouped, 64)
	if !groupedContract.HasOnePass || !groupedContract.HasSolver {
		t.Fatalf("grouped contract omitted phase B: %+v", groupedContract)
	}
}
