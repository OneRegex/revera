package revera

// Host file, outside the Vego subset. It supplies what the subset
// cannot express on purpose: the embedded locale data and the
// callback-style convenience wrappers. Every language a translator
// targets replaces this file with its own minimal runtime.

import (
	_ "embed"
	"sync"
)

//go:embed data.bin
var embeddedLocaleData string

// EmbeddedLocaleData returns the CLDR locale blob compiled into the
// Go build. Pass it to LocaleOpen or LocaleLoad.
func EmbeddedLocaleData() string {
	return embeddedLocaleData
}

// DupMax is the largest supported interval count.
const DupMax = dupMax

var embeddedOnce sync.Once
var embeddedData Locale

// Open resolves a locale name against the embedded data. It mirrors
// go0's locale.Open. The embedded blob is loaded and validated once.
func Open(name, collationType string) (Locale, bool) {
	embeddedOnce.Do(func() {
		embeddedData, _ = LocaleLoad(embeddedLocaleData)
	})
	return LocaleSelect(&embeddedData, name, collationType)
}

// MatchAll calls fn for every non-overlapping match, left to right,
// like go0's MatchAll. The pmatch slice is reused between calls, so
// fn must copy what it keeps. A return of false stops the scan.
func MatchAll(re *Regexp, subject string, limit int, eflags uint32, fn func(pmatch []Match) bool) Error {
	it, err := MatchIterInit(re, limit)
	if err.Code != ErrNone {
		return err
	}
	pmatch := make([]Match, NumSub(re)+1)
	for {
		ok, nerr := MatchIterNext(re, &it, subject, eflags, pmatch)
		if nerr.Code != ErrNone {
			return nerr
		}
		if !ok {
			return noError()
		}
		if !fn(pmatch) {
			return noError()
		}
	}
}

// ReplaceAllFunc returns subject with every non-overlapping match
// replaced by the return value of repl, like go0's ReplaceAllFunc.
func ReplaceAllFunc(re *Regexp, subject string, limit int, eflags uint32, repl func(pmatch []Match) string) (string, Error) {
	var out []uint8
	last := 0
	any := false
	err := MatchAll(re, subject, limit, eflags, func(pmatch []Match) bool {
		if !any {
			out = make([]uint8, 0, len(subject)+len(subject)/8)
			any = true
		}
		out = append(out, subject[last:pmatch[0].So]...)
		out = append(out, repl(pmatch)...)
		last = pmatch[0].Eo
		return true
	})
	if err.Code != ErrNone {
		return "", err
	}
	if !any {
		return subject, noError()
	}
	out = append(out, subject[last:]...)
	return string(out), noError()
}

// CompileWithContract compiles like Compile and also returns the
// resource contract for subjects of at most maxInput bytes.
func CompileWithContract(pattern string, loc Locale, flags uint32, maxInput int) (Regexp, Contract, Error) {
	re, err := Compile(pattern, loc, flags)
	if err.Code != ErrNone {
		var c Contract
		return re, c, err
	}
	return re, ContractFor(&re, maxInput), noError()
}
