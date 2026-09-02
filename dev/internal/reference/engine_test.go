package reference

import (
	"testing"

	"github.com/oneregex/revera/dev/internal/reference/locale"
)

// TestPrepareResetsBase checks that a pooled workspace never starts a run from a generation base that could wrap.
func TestPrepareResetsBase(t *testing.T) {
	if uint64(baseResetLimit)+uint64(subjectLimit)+2*(maxElemAhead+1) >= 1<<32 {
		t.Fatalf("baseResetLimit %d leaves room for a wraparound", baseResetLimit)
	}
	re, err := Compile("a", locale.POSIX(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ws := &engineWS{base: baseResetLimit + 1}
	ws.prepare(re.prog, 0, 2)
	if ws.base != 0 {
		t.Fatalf("base %d was not reset", ws.base)
	}
	ws = &engineWS{base: baseResetLimit}
	ws.prepare(re.prog, 0, 2)
	if ws.base != baseResetLimit {
		t.Fatalf("base %d was reset below the limit", ws.base)
	}
}
