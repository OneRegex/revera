package revera

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestAPIFind(t *testing.T) {
	re := MustNew("([a-z]+)([0-9]*)")
	ok, err := re.MatchString("__abc12__")
	if err != nil || !ok {
		t.Fatalf("MatchString: %v %v", ok, err)
	}
	if re.NumSubexp() != 2 {
		t.Fatalf("NumSubexp: %d", re.NumSubexp())
	}
	span, err := re.FindStringIndex("__abc12__")
	if err != nil || !slices.Equal(span, []int{2, 7}) {
		t.Fatalf("FindStringIndex: %v %v", span, err)
	}
	spans, err := re.FindStringSubmatchIndex("__abc12__")
	if err != nil || !slices.Equal(spans, []int{2, 7, 2, 5, 5, 7}) {
		t.Fatalf("FindStringSubmatchIndex: %v %v", spans, err)
	}
	got, matched, err := re.FindString("__abc12__")
	if err != nil || !matched || got != "abc12" {
		t.Fatalf("FindString: %q %v %v", got, matched, err)
	}
	groups, err := re.FindStringSubmatch("__abc12__")
	if err != nil || !slices.Equal(groups, []string{"abc12", "abc", "12"}) {
		t.Fatalf("FindStringSubmatch: %v %v", groups, err)
	}
}

func TestAPINoMatch(t *testing.T) {
	re := MustNew("z+")
	ok, err := re.MatchString("abc")
	if err != nil || ok {
		t.Fatalf("MatchString: %v %v", ok, err)
	}
	span, err := re.FindStringIndex("abc")
	if err != nil || span != nil {
		t.Fatalf("FindStringIndex: %v %v", span, err)
	}
	groups, err := re.FindStringSubmatch("abc")
	if err != nil || groups != nil {
		t.Fatalf("FindStringSubmatch: %v %v", groups, err)
	}
	all, err := re.FindAllString("abc", -1)
	if err != nil || all != nil {
		t.Fatalf("FindAllString: %v %v", all, err)
	}
}

func TestAPIFindAll(t *testing.T) {
	re := MustNew("(a+)(b*)")
	all, err := re.FindAllString("aab a aabbb", -1)
	if err != nil || !slices.Equal(all, []string{"aab", "a", "aabbb"}) {
		t.Fatalf("FindAllString: %v %v", all, err)
	}
	first, err := re.FindAllString("aab a aabbb", 2)
	if err != nil || !slices.Equal(first, []string{"aab", "a"}) {
		t.Fatalf("FindAllString limited: %v %v", first, err)
	}
	rows, err := re.FindAllStringSubmatch("aab a", -1)
	if err != nil || len(rows) != 2 ||
		!slices.Equal(rows[0], []string{"aab", "aa", "b"}) ||
		!slices.Equal(rows[1], []string{"a", "a", ""}) {
		t.Fatalf("FindAllStringSubmatch: %v %v", rows, err)
	}
	spans, err := re.FindAllStringIndex("aab a", -1)
	if err != nil || len(spans) != 2 || !slices.Equal(spans[0], []int{0, 3}) {
		t.Fatalf("FindAllStringIndex: %v %v", spans, err)
	}
	idx, err := re.FindAllStringSubmatchIndex("aab", -1)
	if err != nil || len(idx) != 1 || !slices.Equal(idx[0], []int{0, 3, 0, 2, 2, 3}) {
		t.Fatalf("FindAllStringSubmatchIndex: %v %v", idx, err)
	}
}

func TestAPIReplace(t *testing.T) {
	re := MustNew("(a+)(b*)")
	out, err := re.ReplaceAllString("xaabyy", "[&:\\2]")
	if err != nil || out != "x[aab:b]yy" {
		t.Fatalf("ReplaceAllString: %q %v", out, err)
	}
	out, err = re.ReplaceAllStringFunc("xaabyy", strings.ToUpper)
	if err != nil || out != "xAAByy" {
		t.Fatalf("ReplaceAllStringFunc: %q %v", out, err)
	}
	out, err = re.ReplaceAllStringFunc("xyz", strings.ToUpper)
	if err != nil || out != "xyz" {
		t.Fatalf("ReplaceAllStringFunc without a match: %q %v", out, err)
	}
}

func TestAPIOptions(t *testing.T) {
	re := MustNew("ab+", CaseInsensitive())
	if ok, err := re.MatchString("ABBB"); err != nil || !ok {
		t.Fatalf("CaseInsensitive: %v %v", ok, err)
	}
	re = MustNew("^b", NewlineSensitive())
	span, err := re.FindStringIndex("a\nbc")
	if err != nil || !slices.Equal(span, []int{2, 3}) {
		t.Fatalf("NewlineSensitive: %v %v", span, err)
	}
	re = MustNew("a+", ShortestMatch())
	span, err = re.FindStringIndex("aaa")
	if err != nil || !slices.Equal(span, []int{0, 1}) {
		t.Fatalf("ShortestMatch: %v %v", span, err)
	}
	re = MustNew("a+", NoCaptures())
	if ok, err := re.MatchString("baa"); err != nil || !ok {
		t.Fatalf("NoCaptures match: %v %v", ok, err)
	}
	if _, err := re.FindStringIndex("baa"); err == nil {
		t.Fatal("NoCaptures must refuse offsets")
	}
}

func TestAPILocale(t *testing.T) {
	cs, err := OpenLocale("cs", "")
	if err != nil {
		t.Fatalf("OpenLocale: %v", err)
	}
	re := MustNew("[[.ch.]]", In(cs))
	if ok, err := re.MatchString("ch"); err != nil || !ok {
		t.Fatalf("collating element: %v %v", ok, err)
	}
	if _, err := OpenLocale("xx-not-there", ""); err == nil {
		t.Fatal("expected an unknown locale to fail")
	}
	names := LocaleNames()
	if len(names) < 1000 || !slices.Contains(names, "cs") {
		t.Fatalf("LocaleNames returned %d names", len(names))
	}
}

func TestAPIErrors(t *testing.T) {
	_, err := New("a(")
	if err == nil {
		t.Fatal("expected a compile error")
	}
	e, ok := err.(Error)
	if !ok || e.Code != ErrBadPat || e.Pos != 2 {
		t.Fatalf("expected ErrBadPat at byte 2, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid regular expression at byte 2") {
		t.Fatalf("error text: %q", err.Error())
	}
	if !strings.Contains(Error{Code: ErrESpace, Pos: -1}.Error(), "capacity") {
		t.Fatal("positionless error text")
	}
}

func TestAPIConcurrentSearch(t *testing.T) {
	re := MustNew("[0-9]+")
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				got, ok, err := re.FindString("ab 1234 cd")
				if err != nil || !ok || got != "1234" {
					t.Errorf("FindString: %q %v %v", got, ok, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
