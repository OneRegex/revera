package main

import (
	"fmt"
	"math/rand"
	"strings"
)

func atom(rng *rand.Rand) string {
	switch rng.Intn(9) {
	case 0:
		return "."
	case 1:
		return "[ab]"
	case 2:
		return "[a-c]"
	case 3:
		return "[^ab]"
	case 4:
		return "[^a-c]"
	default:
		return string(rune('a' + rng.Intn(3)))
	}
}

func pattern(rng *rand.Rand, depth int) string {
	if depth <= 0 {
		return atom(rng)
	}
	switch rng.Intn(11) {
	case 9:
		return "^" + pattern(rng, depth-1)
	case 10:
		return pattern(rng, depth-1) + "$"
	case 0:
		return pattern(rng, depth-1) + "|" + pattern(rng, depth-1)
	case 1:
		dups := []string{"", "*", "+", "?", "{2}", "{0,2}", "{1,}"}
		return "(" + pattern(rng, depth-1) + ")" + dups[rng.Intn(len(dups))]
	case 2:
		return pattern(rng, depth-1) + pattern(rng, depth-1)
	default:
		return atom(rng) + pattern(rng, depth-1)
	}
}

func main() {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 20000; i++ {
		p := pattern(rng, 3)
		var s strings.Builder
		n := rng.Intn(9)
		for j := 0; j < n; j++ {
			s.WriteByte(byte('a' + rng.Intn(3)))
		}
		fmt.Printf("%s\t%s\n", p, s.String())
	}
}
