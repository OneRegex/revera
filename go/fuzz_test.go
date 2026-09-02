package revera

import "testing"

// FuzzEngine is the crash-only fuzz entry point of the engine module.
// It reads the shared input layout that dev/internal/protocol/fuzz.go defines, so a corpus transfers between the targets, and runs compile, exec, replace, iteration and the contract on it.
// Every result is ignored; a crash is the only signal.
// The differential fuzz test of dev/internal/differential compares the same inputs with the reference engine.
func FuzzEngine(f *testing.F) {
	seeds := []string{
		"\x00\x00\x06(a|ab)\x00abcd",
		"\x01\x10\x04[[.ch.]]\x02\\1chch",
		"\x02\x20\x08(a+)(b+)\x04\\2\\1aabb xab",
		"\x08\x03\x0b((a*){4}){4}\x00aaaaaaaa",
		"\x04\x01\x05^a$|b\x01&a\nb",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	var base Locale
	if !localeLoad(&base, EmbeddedLocaleData()) {
		f.Fatal("embedded locale data failed to load")
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		fuzzRun(&base, data)
	})
}

// fuzzRun decodes one input and runs the crash-only procedure.
// Byte 0 holds the compile flags, byte 1 the exec flags with bit 4 selecting the cs locale and bit 5 tr, byte 2 the pattern length; a replacement length and the replacement follow the pattern, and the rest is the subject.
func fuzzRun(base *Locale, data []byte) {
	if len(data) < 3 {
		return
	}
	flags, eflags := uint32(data[0]&0x0f), uint32(data[1]&0x03)
	loc := LocalePOSIX()
	if name := fuzzLocaleName(data[1]); name != "" {
		selected, ok := LocaleSelect(base, name, "")
		if !ok {
			return
		}
		loc = selected
	}
	n := int(data[2])
	rest := data[3:]
	pattern := string(rest[:min(n, len(rest))])
	rest = rest[min(n, len(rest)):]
	var replacement string
	if len(rest) > 0 {
		m := int(rest[0])
		rest = rest[1:]
		replacement = string(rest[:min(m, len(rest))])
		rest = rest[min(m, len(rest)):]
	}
	subject := string(rest)
	re, err := Compile(pattern, loc, flags)
	if err.Code != ErrNone {
		return
	}
	pmatch := make([]Match, NumSub(&re)+1)
	_, _ = Exec(&re, subject, pmatch, eflags)
	_, _ = ReplaceAll(&re, subject, replacement, -1, eflags)
	it, ierr := MatchIterInit(&re, 3)
	if ierr.Code == ErrNone {
		for {
			got, nerr := MatchIterNext(&re, &it, subject, eflags, pmatch)
			if nerr.Code != ErrNone || !got {
				break
			}
		}
	}
	c := ContractFor(&re, len(subject))
	_ = ContractHeapBytes(&c) + ContractStackBytes(&c) + ContractSteps(&c)
}

// fuzzLocaleName reads the locale selection out of the second input byte.
func fuzzLocaleName(b byte) string {
	switch {
	case b&0x10 != 0:
		return "cs"
	case b&0x20 != 0:
		return "tr"
	}
	return ""
}
