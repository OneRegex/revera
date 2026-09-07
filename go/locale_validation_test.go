package revera

import (
	"encoding/binary"
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
