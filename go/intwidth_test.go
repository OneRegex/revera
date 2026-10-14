package revera

import (
	"slices"
	"testing"
)

func TestSaturatingLengthArithmetic(t *testing.T) {
	for _, tc := range []struct {
		a, b         int
		sum, product int
	}{
		{0, lenInf, lenInf, 0},
		{1, lenInf - 1, lenInf, lenInf - 1},
		{2, lenInf / 2, lenInf/2 + 2, lenInf},
		{3, lenInf/3 + 1, lenInf/3 + 4, lenInf},
		{lenInf, lenInf, lenInf, lenInf},
	} {
		if got := satAdd(tc.a, tc.b); got != tc.sum {
			t.Errorf("satAdd(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.sum)
		}
		if got := satMul(tc.a, tc.b); got != tc.product {
			t.Errorf("satMul(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.product)
		}
	}
}

func TestSaturatedLengthsKeepResults(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		subject string
		want    []Match
	}{
		{"(a*)(b*)", "ab", []Match{{0, 2}, {0, 1}, {1, 2}}},
		{"(a*){4}", "aaa", []Match{{0, 3}, {3, 3}}},
	} {
		re, err := Compile(tc.pattern, LocalePOSIX(), 0)
		if err.Code != ErrNone {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		pmatch := make([]Match, NumSub(&re)+1)
		matched, err := Exec(&re, tc.subject, pmatch, 0)
		if err.Code != ErrNone || matched != (tc.want != nil) || matched && !slices.Equal(pmatch, tc.want) {
			t.Errorf("Exec(%q, %q) = %v, %v, %v; want %v", tc.pattern, tc.subject, matched, err, pmatch, tc.want)
		}
	}
}
