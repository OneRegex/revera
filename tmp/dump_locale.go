package main

import (
	"fmt"

	"revera/locale"
)

func dump(name, ctype string) {
	l, ok := locale.Open(name, ctype)
	if !ok {
		fmt.Printf("open %s/%s FAIL\n", name, ctype)
		return
	}
	fmt.Printf("locale %s/%s\n", name, ctype)
	for cp := rune(0); cp < 0x500; cp += 7 {
		mask := 0
		for c := 0; c < 12; c++ {
			if l.IsClass(locale.Class(c), cp) {
				mask |= 1 << c
			}
		}
		fmt.Printf("c %04x %03x %04x %04x\n", cp, mask, l.ToUpper(cp), l.ToLower(cp))
	}
	for cp := rune(0x1E00); cp < 0x1F00; cp += 3 {
		fmt.Printf("u %04x %04x %04x\n", cp, l.ToUpper(cp), l.ToLower(cp))
	}
	pairs := [][2]string{
		{"a", "à"}, {"a", "á"}, {"a", "A"}, {"o", "ö"}, {"u", "ü"}, {"c", "ç"}, {"e", "é"},
		{"s", "ß"}, {"n", "ñ"}, {"a", "b"}, {"i", "ı"}, {"i", "İ"},
	}
	for _, p := range pairs {
		eq := l.PrimaryEqual([]rune(p[0]), []rune(p[1]))
		v := 0
		if eq {
			v = 1
		}
		fmt.Printf("eq %s %s %d\n", p[0], p[1], v)
	}
	elems := []string{"ch", "dz", "ll", "dzs", "ngb", "ab"}
	for _, e := range elems {
		ce := 0
		if l.IsCollatingElement([]rune(e)) {
			ce = 1
		}
		fmt.Printf("ce %s %d %d\n", e, ce, l.CollatingPrefix([]rune(e)))
	}
}

func main() {
	locales := [][2]string{
		{"C", ""}, {"en", ""}, {"en-US", ""}, {"fr", ""}, {"de", ""}, {"de", "phonebook"},
		{"tr", ""}, {"az", ""}, {"cs", ""}, {"hu", ""}, {"es", ""}, {"es", "traditional"},
		{"da", ""}, {"sv", ""}, {"ja", ""}, {"zh", ""}, {"zh", "pinyin"}, {"vi", ""}, {"sl", ""},
	}
	for _, pair := range locales {
		dump(pair[0], pair[1])
	}
}
