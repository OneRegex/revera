// Package codesize measures machine code in built binaries.
// It reads the symbol table of a Mach-O or ELF file with the standard library, so it needs no external tool.
// Mach-O symbols carry no size, so the size of a function is the distance to the next symbol in its section.
package codesize

import (
	"debug/elf"
	"debug/macho"
	"fmt"
	"regexp"
	"sort"
)

// Symbol is one function with the bytes of code it occupies.
// Aliases are the other names at the same address; they share the size and never count twice.
type Symbol struct {
	Name    string
	Aliases []string
	Size    uint64
}

// Matches reports whether the name or any alias matches the expression.
func (s Symbol) Matches(re *regexp.Regexp) bool {
	if re.MatchString(s.Name) {
		return true
	}
	for _, alias := range s.Aliases {
		if re.MatchString(alias) {
			return true
		}
	}
	return false
}

// Report describes the code of one binary.
type Report struct {
	// TextBytes is the size of the executable sections.
	TextBytes uint64
	// Functions lists every function symbol with its size.
	Functions []Symbol
}

// Analyze reads a binary and sizes its functions.
func Analyze(path string) (Report, error) {
	if f, err := macho.Open(path); err == nil {
		defer func() { _ = f.Close() }()
		return analyzeMacho(f)
	}
	f, err := elf.Open(path)
	if err != nil {
		return Report{}, fmt.Errorf("%s: not a Mach-O or ELF file", path)
	}
	defer func() { _ = f.Close() }()
	return analyzeELF(f)
}

func analyzeMacho(f *macho.File) (Report, error) {
	var r Report
	if f.Symtab == nil {
		return r, fmt.Errorf("binary has no symbol table")
	}
	text := map[uint8]*macho.Section{}
	for i, s := range f.Sections {
		if s.Name == "__text" || s.Flags&0x80000000 != 0 {
			text[uint8(i+1)] = s
			r.TextBytes += s.Size
		}
	}
	type entry struct {
		name  string
		value uint64
		sect  uint8
	}
	var entries []entry
	for _, sym := range f.Symtab.Syms {
		if sym.Type&0xe0 != 0 || sym.Type&0x0e != 0x0e {
			continue
		}
		if text[sym.Sect] == nil {
			continue
		}
		entries = append(entries, entry{name: sym.Name, value: sym.Value, sect: sym.Sect})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].sect != entries[j].sect {
			return entries[i].sect < entries[j].sect
		}
		return entries[i].value < entries[j].value
	})
	for i := 0; i < len(entries); {
		e := entries[i]
		sym := Symbol{Name: e.name}
		j := i + 1
		for j < len(entries) && entries[j].sect == e.sect && entries[j].value == e.value {
			sym.Aliases = append(sym.Aliases, entries[j].name)
			j++
		}
		end := text[e.sect].Addr + text[e.sect].Size
		if j < len(entries) && entries[j].sect == e.sect {
			end = entries[j].value
		}
		i = j
		if end < e.value {
			continue
		}
		sym.Size = end - e.value
		r.Functions = append(r.Functions, sym)
	}
	return r, nil
}

func analyzeELF(f *elf.File) (Report, error) {
	var r Report
	for _, s := range f.Sections {
		if s.Flags&elf.SHF_EXECINSTR != 0 {
			r.TextBytes += s.Size
		}
	}
	syms, err := f.Symbols()
	if err != nil {
		return r, fmt.Errorf("binary has no symbol table: %w", err)
	}
	for _, sym := range syms {
		if elf.ST_TYPE(sym.Info) != elf.STT_FUNC {
			continue
		}
		r.Functions = append(r.Functions, Symbol{Name: sym.Name, Size: sym.Size})
	}
	return r, nil
}

// Sum adds the sizes of the functions that match keep and, when drop is not nil, do not match drop.
// It returns the bytes and the number of functions.
func (r Report) Sum(keep, drop *regexp.Regexp) (uint64, int) {
	var bytes uint64
	count := 0
	for _, fn := range r.Functions {
		if fn.Matches(keep) && (drop == nil || !fn.Matches(drop)) {
			bytes += fn.Size
			count++
		}
	}
	return bytes, count
}
