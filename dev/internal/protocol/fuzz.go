package protocol

// Host file, outside the Vego subset.
// It defines the shared fuzz input format and runs one input through the engine.
// Every target implements the same procedure in its fuzz entry point, so one seed pack drives all of them.
// The differential fuzz test in dev/internal/differential adds a check against the reference engine on top of it.
//
// Layout of an input:
//
//	byte 0      compile flags, masked with 0x0f
//	byte 1      bits 0 and 1 are the exec flags; bit 4 selects the cs locale, else bit 5 selects tr
//	byte 2      n, the pattern length
//	n bytes     the pattern
//	1 byte      m, the replacement length
//	m bytes     the replacement
//	rest        the subject
//
// An input shorter than three bytes does nothing.
// A pattern or replacement that runs past the end of the input is cut there.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"

	"github.com/oneregex/revera/go"
)

// FuzzInput is one decoded fuzz input.
type FuzzInput struct {
	Flags       uint32
	EFlags      uint32
	Locale      string
	Pattern     string
	Replacement string
	Subject     string
}

const (
	fuzzLocaleCS = 0x10
	fuzzLocaleTR = 0x20
)

// DecodeFuzzInput splits raw bytes into the fields of the format.
// It reports false when the input is too short to hold a header.
func DecodeFuzzInput(data []byte) (FuzzInput, bool) {
	if len(data) < 3 {
		return FuzzInput{}, false
	}
	in := FuzzInput{Flags: uint32(data[0] & 0x0f), EFlags: uint32(data[1] & 0x03)}
	switch {
	case data[1]&fuzzLocaleCS != 0:
		in.Locale = "cs"
	case data[1]&fuzzLocaleTR != 0:
		in.Locale = "tr"
	}
	n := int(data[2])
	rest := data[3:]
	in.Pattern = string(rest[:min(n, len(rest))])
	rest = rest[min(n, len(rest)):]
	if len(rest) > 0 {
		m := int(rest[0])
		rest = rest[1:]
		in.Replacement = string(rest[:min(m, len(rest))])
		rest = rest[min(m, len(rest)):]
	}
	in.Subject = string(rest)
	return in, true
}

// EncodeFuzzInput builds the raw bytes of an input.
// A pattern or replacement longer than 255 bytes is cut.
func EncodeFuzzInput(in FuzzInput) []byte {
	pattern := in.Pattern
	if len(pattern) > 255 {
		pattern = pattern[:255]
	}
	replacement := in.Replacement
	if len(replacement) > 255 {
		replacement = replacement[:255]
	}
	b1 := byte(in.EFlags & 0x03)
	switch in.Locale {
	case "cs":
		b1 |= fuzzLocaleCS
	case "tr":
		b1 |= fuzzLocaleTR
	}
	out := make([]byte, 0, 4+len(pattern)+len(replacement)+len(in.Subject))
	out = append(out, byte(in.Flags&0x0f), b1, byte(len(pattern)))
	out = append(out, pattern...)
	out = append(out, byte(len(replacement)))
	out = append(out, replacement...)
	out = append(out, in.Subject...)
	return out
}

// FuzzRun runs one input through compile, exec, replace, iteration and the contract.
// It ignores every result; a crash is the only signal.
// base is the loaded locale data, from LocaleLoad.
func FuzzRun(base *revera.Locale, data []byte) {
	in, ok := DecodeFuzzInput(data)
	if !ok {
		return
	}
	loc, ok := LocaleByName(base, in.Locale)
	if !ok {
		return
	}
	re, err := revera.Compile(in.Pattern, loc, in.Flags)
	if err.Code != revera.ErrNone {
		return
	}
	pmatch := make([]revera.Match, revera.NumSub(&re)+1)
	_, _ = revera.Exec(&re, in.Subject, pmatch, in.EFlags)
	_, _ = revera.ReplaceAll(&re, in.Subject, in.Replacement, -1, in.EFlags)
	it, ierr := revera.MatchIterInit(&re, 3)
	if ierr.Code == revera.ErrNone {
		for {
			got, nerr := revera.MatchIterNext(&re, &it, in.Subject, in.EFlags, pmatch)
			if nerr.Code != revera.ErrNone || !got {
				break
			}
		}
	}
	c := revera.ContractFor(&re, len(in.Subject))
	_ = revera.ContractHeapBytes(&c) + revera.ContractStackBytes(&c) + revera.ContractSteps(&c)
}

// FuzzSeeds builds the deterministic seed inputs of the fuzz entry points.
// They come from the fixed corpus, the multi-element and replacement tables, and a seeded random sample.
func FuzzSeeds() [][]byte {
	var seeds [][]byte
	add := func(in FuzzInput) {
		seeds = append(seeds, EncodeFuzzInput(in))
	}
	subjects := []string{"", "ab", "aabb", "weeknights", "a\nb", "\xff", "éèe"}
	for _, flags := range FixedFlagSets {
		for _, pattern := range FixedPatterns {
			if pattern == HeavyPattern {
				continue
			}
			for i, subject := range subjects {
				add(FuzzInput{Flags: flags, EFlags: uint32(i % 4), Pattern: pattern, Subject: subject, Replacement: `\1-&`})
			}
		}
	}
	for _, flags := range []uint32{0, revera.FlagICase, revera.FlagMinimal} {
		for _, pattern := range MultiElementPatterns {
			for _, subject := range []string{"", "ch", "chch", "cHx", "hcch"} {
				add(FuzzInput{Flags: flags, Locale: "cs", Pattern: pattern, Subject: subject, Replacement: "x"})
			}
		}
	}
	for _, pattern := range LocalePatterns {
		for _, subject := range LocaleSubjects {
			add(FuzzInput{Flags: revera.FlagICase, Locale: "tr", Pattern: pattern, Subject: subject, Replacement: `\0`})
		}
	}
	for _, c := range ReplaceCases {
		add(FuzzInput{Pattern: c.Pattern, Subject: c.Subject, Replacement: c.Replacement})
	}
	rng := rand.New(rand.NewSource(11))
	alphabets := []string{"abc", "ab\nc", "abcABC"}
	for i := range 600 {
		pattern := GenPattern(rng, 3)
		alphabet := alphabets[i%len(alphabets)]
		for range 2 {
			add(FuzzInput{
				Flags:       uint32(rng.Intn(16)),
				EFlags:      uint32(rng.Intn(4)),
				Pattern:     pattern,
				Subject:     GenSubject(rng, alphabet, 8),
				Replacement: GenSubject(rng, `a\&1`, 3),
			})
		}
	}
	return seeds
}

// fuzzRecordMax bounds one record of a pack.
// A fuzz input is a few hundred bytes; a longer length field is a corrupt pack, not a large input.
const fuzzRecordMax = 1 << 24

// WriteFuzzPack writes inputs as length-prefixed records.
// Each record is a 4-byte little-endian length followed by the bytes.
// Every fuzzcase binary reads this format.
func WriteFuzzPack(w io.Writer, inputs [][]byte) error {
	var header [4]byte
	for i, in := range inputs {
		if len(in) > fuzzRecordMax {
			return fmt.Errorf("fuzz pack record %d: %d bytes is over the record limit", i, len(in))
		}
		binary.LittleEndian.PutUint32(header[:], uint32(len(in)))
		if _, err := w.Write(header[:]); err != nil {
			return err
		}
		if _, err := w.Write(in); err != nil {
			return err
		}
	}
	return nil
}

// ReadFuzzPack reads the records that WriteFuzzPack wrote.
func ReadFuzzPack(r io.Reader) ([][]byte, error) {
	var inputs [][]byte
	var header [4]byte
	for {
		_, err := io.ReadFull(r, header[:])
		if errors.Is(err, io.EOF) {
			return inputs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("fuzz pack record %d: truncated header", len(inputs))
		}
		n := binary.LittleEndian.Uint32(header[:])
		if n > fuzzRecordMax {
			return nil, fmt.Errorf("fuzz pack record %d: length %d is over the record limit", len(inputs), n)
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("fuzz pack record %d: truncated body", len(inputs))
		}
		inputs = append(inputs, body)
	}
}
