package revera

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestLocaleRejectsInvalidCaseScalar(t *testing.T) {
	base, ok := LocaleLoad(EmbeddedLocaleData())
	if !ok {
		t.Fatal("embedded locale failed to load")
	}
	for _, sec := range []int{secCaseDefault, secCaseTurkic} {
		for _, value := range []uint32{0xffffffff, 0xd800, 0x110000} {
			raw := []byte(EmbeddedLocaleData())
			off := base.sec[sec].Off
			r := int32(binary.LittleEndian.Uint32(raw[off:]))
			binary.LittleEndian.PutUint32(raw[off+4:], value)
			name := "en"
			if sec == secCaseTurkic {
				name = "tr"
			}
			loc, accepted := LocaleOpen(string(raw), name, "")
			if accepted {
				re, err := Compile(runesToString([]int32{r}), loc, FlagICase)
				if err.Code != ErrNone {
					t.Fatalf("accepted invalid case scalar %#x; compile failed: %v", value, err)
				}
				matched, err := Exec(&re, "\xff", nil, 0)
				t.Errorf("section %d accepted invalid case scalar %#x; invalid UTF-8 matched=%v, error=%v", sec, value, matched, err)
			}
		}
	}
}

func TestLocaleRejectsInvalidScalarSections(t *testing.T) {
	base, ok := LocaleLoad(EmbeddedLocaleData())
	if !ok {
		t.Fatal("embedded locale failed to load")
	}
	for _, sec := range []int{
		secCaseDefault, secCaseTurkic,
		secInvUpperDefault, secInvLowerDefault, secInvUpperTurkic, secInvLowerTurkic,
		secSeqCodepoints,
	} {
		fields := 1
		if sec == secCaseDefault || sec == secCaseTurkic {
			fields = 3
		} else if sec != secSeqCodepoints {
			fields = 2
		}
		for field := 0; field < fields; field++ {
			for _, value := range []uint32{0xffffffff, 0xd800, 0x110000} {
				raw := []byte(EmbeddedLocaleData())
				binary.LittleEndian.PutUint32(raw[base.sec[sec].Off+field*4:], value)
				if _, accepted := LocaleLoad(string(raw)); accepted {
					t.Errorf("section %d field %d accepted non-scalar %#x", sec, field, value)
				}
			}
		}
	}
}

func TestLocaleRejectsUnderstatedSequenceMaximum(t *testing.T) {
	base, ok := LocaleLoad(EmbeddedLocaleData())
	if !ok {
		t.Fatal("embedded locale failed to load")
	}
	raw := []byte(EmbeddedLocaleData())
	binary.LittleEndian.PutUint32(raw[base.sec[secScalars].Off:], 1)
	loc, accepted := LocaleOpen(string(raw), "cs", "")
	if accepted {
		re, err := Compile("[[.ch.]]", loc, 0)
		if err.Code != ErrNone {
			t.Fatalf("accepted inconsistent sequence maximum; compile failed: %v", err)
		}
		matched, err := Exec(&re, "ch", nil, 0)
		t.Fatalf("accepted maximum 1 for the two-character ch element; matched=%v, error=%v", matched, err)
	}
}

func TestLocaleRejectsOutOfRangeFields(t *testing.T) {
	base, ok := LocaleLoad(EmbeddedLocaleData())
	if !ok {
		t.Fatal("embedded locale failed to load")
	}
	// Some values keep their low bits valid, so only a check of the whole stored value catches them.
	// Values with the top bit set would turn negative as a 32-bit int.
	profile := base.sec[secCollationProfiles].Off
	locale := base.sec[secLocales].Off
	for _, tc := range []struct {
		name  string
		off   int
		value uint32
	}{
		{"first section length", len("RVLOC001"), 0x80000000},
		{"sequence offset", base.sec[secSequences].Off, 0x80000000},
		{"contraction sequence ID", base.sec[secRootContractions].Off, firstSequenceID + 0x80000000},
		{"profile override first", profile, 0xffffffff},
		{"profile add first", profile + 8, 0x80000000},
		{"profile remove count", profile + 20, 0xffffffff},
		{"type name offset", base.sec[secTypeNameOffsets].Off, 0x80000000},
		{"locale name offset", base.sec[secLocaleNameOffsets].Off, 0x80000000},
		{"locale type first", locale + 4, 0xffffffff},
		{"locale case profile", locale + 12, 2},
		{"locale case profile", locale + 12, 0x101},
		{"locale default collation", locale + 16, 0x10000},
		{"locale default collation", locale + 16, 0x80000000},
	} {
		raw := []byte(EmbeddedLocaleData())
		binary.LittleEndian.PutUint32(raw[tc.off:], tc.value)
		if _, accepted := LocaleLoad(string(raw)); accepted {
			t.Errorf("%s accepted %#x", tc.name, tc.value)
		}
	}
}

func TestLocaleRejectsProfilesPastSixteenBits(t *testing.T) {
	base, ok := LocaleLoad(EmbeddedLocaleData())
	if !ok {
		t.Fatal("embedded locale failed to load")
	}
	profiles := sectionLen(&base, secCollationProfiles) / 24
	// grow adds empty profile rows and makes the last one the default of the first locale.
	grow := func(rows int) string {
		blob := EmbeddedLocaleData()
		end := base.sec[secCollationProfiles].End
		raw := []byte(blob[:end] + strings.Repeat("\x00", 24*rows) + blob[end:])
		lengthAt := base.sec[secCollationProfiles].Off - 4
		binary.LittleEndian.PutUint32(raw[lengthAt:], uint32(24*(profiles+rows)))
		binary.LittleEndian.PutUint32(raw[base.sec[secLocales].Off+24*rows+16:], uint32(profiles+rows-1))
		return string(raw)
	}
	if _, accepted := LocaleLoad(grow(1<<16 - profiles)); !accepted {
		t.Fatal("rejected exactly 65,536 profiles")
	}
	if _, accepted := LocaleLoad(grow(1<<16 - profiles + 1)); accepted {
		t.Fatal("accepted a default collation of 65,536, which a 16-bit index wraps to 0")
	}
}
