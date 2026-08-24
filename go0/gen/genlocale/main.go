// Command genlocale converts the generated C locale tables into a compact
// binary blob for the Go locale package.
//
// The input is src/rv_locale_data.inc at the repository root. The output is
// locale/data.bin. The blob is a sequence of sections. Each section is a
// little-endian u32 byte length followed by the section payload. The locale
// package knows the section order.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

var arrayPattern = regexp.MustCompile(
	`(?s)static const \w+ (rv_\w+)\[\] = \{(.*?)\};`)

var scalarPattern = regexp.MustCompile(
	`static const \w+ (rv_\w+) = (\d+);`)

var numberPattern = regexp.MustCompile(`0x[0-9a-fA-F]+|\d+`)

func parseTables(source string) (map[string][]uint64, map[string]uint64, error) {
	tables := make(map[string][]uint64)
	scalars := make(map[string]uint64)

	for _, match := range arrayPattern.FindAllStringSubmatch(source, -1) {
		name := match[1]
		body := match[2]
		numbers := numberPattern.FindAllString(body, -1)
		values := make([]uint64, 0, len(numbers))
		for _, text := range numbers {
			value, err := strconv.ParseUint(text, 0, 64)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", name, err)
			}
			values = append(values, value)
		}
		tables[name] = values
	}
	for _, match := range scalarPattern.FindAllStringSubmatch(source, -1) {
		value, err := strconv.ParseUint(match[2], 0, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", match[1], err)
		}
		scalars[match[1]] = value
	}
	return tables, scalars, nil
}

func need(tables map[string][]uint64, name string) ([]uint64, error) {
	values, ok := tables[name]
	if !ok {
		return nil, fmt.Errorf("missing table %s", name)
	}
	return values, nil
}

// checkCount verifies one declared row count against the parsed flat length.
func checkCount(scalars map[string]uint64, name string, flat, width int) error {
	declared, ok := scalars[name+"_count"]
	if !ok {
		return fmt.Errorf("missing %s_count", name)
	}
	if flat%width != 0 || uint64(flat/width) != declared {
		return fmt.Errorf("%s: %d values, width %d, declared %d",
			name, flat, width, declared)
	}
	return nil
}

// caseInverse builds sorted (target, source) pairs for one direction of a
// merged case map. rows holds (cp, upper, lower) triples. Identity mappings
// are skipped; the runtime always considers the character itself.
func caseInverse(rows [][3]uint32, toUpper bool) []uint64 {
	type pair struct{ target, source uint32 }
	pairs := make([]pair, 0, len(rows))
	for _, row := range rows {
		target := row[2]
		if toUpper {
			target = row[1]
		}
		if target != row[0] {
			pairs = append(pairs, pair{target, row[0]})
		}
	}
	sortPairs := func(a, b pair) bool {
		if a.target != b.target {
			return a.target < b.target
		}
		return a.source < b.source
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && sortPairs(pairs[j], pairs[j-1]); j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	out := make([]uint64, 0, 2*len(pairs))
	for _, p := range pairs {
		out = append(out, uint64(p.target), uint64(p.source))
	}
	return out
}

// mergedCaseRows applies the turkic override rows on top of the default
// rows and returns the merged (cp, upper, lower) triples.
func mergedCaseRows(defaults, overrides []uint64) [][3]uint32 {
	rows := make([][3]uint32, 0, len(defaults)/3)
	for i := 0; i+3 <= len(defaults); i += 3 {
		rows = append(rows, [3]uint32{
			uint32(defaults[i]), uint32(defaults[i+1]), uint32(defaults[i+2])})
	}
	for i := 0; i+3 <= len(overrides); i += 3 {
		cp := uint32(overrides[i])
		replaced := false
		for j := range rows {
			if rows[j][0] == cp {
				rows[j][1] = uint32(overrides[i+1])
				rows[j][2] = uint32(overrides[i+2])
				replaced = true
				break
			}
		}
		if !replaced {
			rows = append(rows, [3]uint32{cp,
				uint32(overrides[i+1]), uint32(overrides[i+2])})
		}
	}
	return rows
}

func appendSection(blob []byte, payload []byte) []byte {
	blob = binary.LittleEndian.AppendUint32(blob, uint32(len(payload)))
	return append(blob, payload...)
}

func encode16(values []uint64) []byte {
	out := make([]byte, 0, 2*len(values))
	for _, value := range values {
		out = binary.LittleEndian.AppendUint16(out, uint16(value))
	}
	return out
}

func encode32(values []uint64) []byte {
	out := make([]byte, 0, 4*len(values))
	for _, value := range values {
		out = binary.LittleEndian.AppendUint32(out, uint32(value))
	}
	return out
}

func encodeBytes(values []uint64) []byte {
	out := make([]byte, 0, len(values))
	for _, value := range values {
		out = append(out, byte(value))
	}
	return out
}

func run(inputPath, outputPath string) error {
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	tables, scalars, err := parseTables(string(raw))
	if err != nil {
		return err
	}

	for _, value := range tables {
		for _, element := range value {
			if element > 0xffffffff {
				return fmt.Errorf("value does not fit in u32")
			}
		}
	}

	type section struct {
		name  string
		width int // row width in values; 0 means a byte pool
		bits  int // element size in bits for width-1 tables
	}
	layout := []section{
		{"rv_ctype_stage1", 1, 16},
		{"rv_ctype_blocks", 1, 16},
		{"rv_case_default", 3, 32},
		{"rv_case_turkic", 3, 32},
		{"rv_sequence_codepoints", 1, 32},
		{"rv_sequences", 2, 32},
		{"rv_root_contractions", 1, 32},
		{"rv_root_equivalences", 2, 32},
		{"rv_collation_overrides", 2, 32},
		{"rv_contraction_adds", 1, 32},
		{"rv_contraction_removes", 1, 32},
		{"rv_collation_profiles", 6, 32},
		{"rv_type_names", 0, 8},
		{"rv_type_name_offsets", 1, 32},
		{"rv_locale_names", 0, 8},
		{"rv_locale_name_offsets", 1, 32},
		{"rv_locales", 5, 32},
		{"rv_locale_types", 2, 16},
	}

	counted := map[string]int{
		"rv_case_default":        3,
		"rv_case_turkic":         3,
		"rv_sequence_codepoints": 1,
		"rv_sequences":           2,
		"rv_root_contractions":   1,
		"rv_root_equivalences":   2,
		"rv_collation_overrides": 2,
		"rv_contraction_adds":    1,
		"rv_contraction_removes": 1,
		"rv_collation_profiles":  6,
		"rv_locales":             5,
		"rv_locale_types":        2,
	}

	blob := []byte("RVLOC001")
	for _, entry := range layout {
		values, err := need(tables, entry.name)
		if err != nil {
			return err
		}
		if width, ok := counted[entry.name]; ok {
			if err := checkCount(scalars, entry.name, len(values), width); err != nil {
				return err
			}
		}
		var payload []byte
		switch {
		case entry.width == 0:
			payload = encodeBytes(values)
		case entry.bits == 16:
			payload = encode16(values)
		default:
			payload = encode32(values)
		}
		blob = appendSection(blob, payload)
	}

	defaultRows := mergedCaseRows(tables["rv_case_default"], nil)
	turkicRows := mergedCaseRows(tables["rv_case_default"], tables["rv_case_turkic"])
	blob = appendSection(blob, encode32(caseInverse(defaultRows, true)))
	blob = appendSection(blob, encode32(caseInverse(defaultRows, false)))
	blob = appendSection(blob, encode32(caseInverse(turkicRows, true)))
	blob = appendSection(blob, encode32(caseInverse(turkicRows, false)))

	maxSequence, ok := scalars["rv_max_sequence_length"]
	if !ok {
		return fmt.Errorf("missing rv_max_sequence_length")
	}
	nameOffsets := tables["rv_locale_name_offsets"]
	declaredNames := scalars["rv_locale_name_offsets_count"]
	if uint64(len(nameOffsets)) != declaredNames {
		return fmt.Errorf("locale name offset count mismatch")
	}
	typeOffsets := tables["rv_type_name_offsets"]
	declaredTypes := scalars["rv_type_name_offsets_count"]
	if uint64(len(typeOffsets)) != declaredTypes {
		return fmt.Errorf("type name offset count mismatch")
	}
	blob = appendSection(blob, encode32([]uint64{maxSequence}))

	if err := os.WriteFile(outputPath, blob, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d bytes, %d sections\n",
		outputPath, len(blob), len(layout)+5)
	return nil
}

func main() {
	input := "../src/rv_locale_data.inc"
	output := "locale/data.bin"
	if len(os.Args) > 1 {
		input = os.Args[1]
	}
	if len(os.Args) > 2 {
		output = os.Args[2]
	}
	if err := run(input, output); err != nil {
		fmt.Fprintln(os.Stderr, "genlocale:", err)
		os.Exit(1)
	}
}
