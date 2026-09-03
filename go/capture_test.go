package revera

import "testing"

// TestCmpCandAfterFailure checks that a candidate comparison ends once the solver has failed.
func TestCmpCandAfterFailure(t *testing.T) {
	re, err := Compile("x*?(a|b)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatal(err)
	}
	var s capSolver
	s.ctrA = make([]int, re.minSlots)
	s.ctrB = make([]int, re.minSlots)
	seedArenas(&s)
	s.failed = true
	// Tree 0 is the failure sentinel, and it names itself as its only child.
	s.trees[0] = ptree{n: re.root, i: 0, j: 1, kidsOff: 0, kidsLen: 1}
	s.kidStore[0] = 0
	if got := cmpCand(&s, &re, 0, 0); got <= 0 {
		t.Fatalf("cmpCand after failure = %d, want a positive result", got)
	}
}

func TestOnePassInconsistencyFailsClosed(t *testing.T) {
	re, err := Compile("(a|.)", LocalePOSIX(), 0)
	if err.Code != ErrNone {
		t.Fatalf("Compile failed: %v", err)
	}
	if re.onePass {
		t.Fatal("ambiguous expression must not be one-pass")
	}
	re.onePass = true
	matched, execErr := Exec(&re, "a", make([]Match, re.nsub+1), 0)
	if matched || execErr.Code != ErrESpace {
		t.Fatalf("inconsistent one-pass walk returned matched=%v, error=%v", matched, execErr)
	}
}
