// Command json2c converts the Vego JSON form into a C11 header and source pair.
// The output includes the hand-written runtime vg.h.
// That runtime supplies the vg_str value type, the vg_arena allocator, and the Go arithmetic helpers.
// C has no templates, so the slice machinery is monomorphized here, one family of static inline helpers per element type.
// Every function that allocates receives an arena pointer as its first parameter, "mem".
// The output holds no global mutable state.
//
// C has no lambdas and no statement expressions.
// Where the Go evaluation order needs pinning, the printer therefore hoists operands into temporaries.
// The temporaries become prelude statements in front of the statement that consumes the expression.
//
// Usage:
//
//	json2c [-hdr engine.h] [-src engine.c] [-prefix name] input.json
//
// Every global identifier carries the prefix, which defaults to the Vego package name.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"revera1/vegoc"
)

func fatal(args ...any) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func main() {
	hdr := flag.String("hdr", "", "output header (default stdout)")
	src := flag.String("src", "", "output source (default stdout)")
	prefix := flag.String("prefix", "", "global identifier prefix (default the package name)")
	flag.Parse()
	if flag.NArg() != 1 {
		fatal("usage: json2c [-hdr engine.h] [-src engine.c] [-prefix name] input.json")
	}
	p, err := vegoc.LoadFile(flag.Arg(0))
	if err != nil {
		fatal(err)
	}
	g := &gen{p: p, hdrName: "engine.h", prefix: p.Package}
	if *hdr != "" {
		g.hdrName = filepath.Base(*hdr)
	}
	if *prefix != "" {
		g.prefix = *prefix
	}
	header, source := g.files()
	write := func(path, text string) {
		if path == "" {
			os.Stdout.WriteString(text)
			return
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			fatal(err)
		}
	}
	write(*hdr, header)
	write(*src, source)
}

type gen struct {
	p       *vegoc.Program
	b       strings.Builder
	fn      *vegoc.FuncDecl
	tmp     int
	used    map[string]bool
	pre     []string
	depth   int
	hdrName string
	prefix  string

	sliceOrder []string
	sliceTypes map[string]*vegoc.Type
	arrayOrder []string
	arrayTypes map[string]*vegoc.Type
}

// cReserved holds C11 keywords, standard macro names, and runtime names that a local or field identifier must not collide with.
// Global identifiers carry the package prefix and need no renaming.
var cReserved = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true,
	"const": true, "continue": true, "default": true, "do": true,
	"double": true, "else": true, "enum": true, "extern": true,
	"float": true, "for": true, "goto": true, "if": true,
	"inline": true, "int": true, "long": true, "register": true,
	"restrict": true, "return": true, "short": true, "signed": true,
	"sizeof": true, "static": true, "struct": true, "switch": true,
	"typedef": true, "union": true, "unsigned": true, "void": true,
	"volatile": true, "while": true, "bool": true, "true": true,
	"false": true, "NULL": true, "EOF": true, "errno": true,
	"stdin": true, "stdout": true, "stderr": true, "assert": true,
	"minor": true, "major": true, "index": true, "mem": true,
}

func ident(name string) string {
	if cReserved[name] || strings.HasPrefix(name, "vego_") || strings.HasPrefix(name, "vg_") {
		return "vego_" + name
	}
	return name
}

// global names a top-level constant, variable, type or function.
func (g *gen) global(name string) string {
	return g.prefix + "_" + name
}

// mangle folds a type into the identifier fragment the monomorphized C names use.
func (g *gen) mangle(t *vegoc.Type) string {
	switch t.K {
	case vegoc.KBool:
		return "bool"
	case vegoc.KU8:
		return "u8"
	case vegoc.KU16:
		return "u16"
	case vegoc.KU32:
		return "u32"
	case vegoc.KU64:
		return "u64"
	case vegoc.KI32:
		return "i32"
	case vegoc.KI64, vegoc.KInt:
		return "i64"
	case vegoc.KStr:
		return "Str"
	case vegoc.KSlice:
		return "slice_" + g.mangle(t.Elem)
	case vegoc.KArray:
		if !t.ALenSet {
			fatal("cannot mangle an unresolved array length")
		}
		return "arr_" + g.mangle(t.Elem) + "_" + strconv.FormatInt(t.ALenVal, 10)
	case vegoc.KStruct:
		return t.Name
	}
	fatal("cannot mangle type", t)
	return ""
}

func (g *gen) sliceName(t *vegoc.Type) string {
	return g.global(g.mangle(t))
}

func (g *gen) arrName(t *vegoc.Type) string {
	return g.global(g.mangle(t))
}

func (g *gen) typ(t *vegoc.Type) string {
	switch t.K {
	case vegoc.KBool:
		return "bool"
	case vegoc.KU8:
		return "uint8_t"
	case vegoc.KU16:
		return "uint16_t"
	case vegoc.KU32:
		return "uint32_t"
	case vegoc.KU64:
		return "uint64_t"
	case vegoc.KI32:
		return "int32_t"
	case vegoc.KI64, vegoc.KInt:
		return "int64_t"
	case vegoc.KStr:
		return "vg_str"
	case vegoc.KSlice:
		return g.sliceName(t)
	case vegoc.KArray:
		return g.arrName(t)
	case vegoc.KStruct:
		return g.global(t.Name)
	case vegoc.KPtr:
		return g.global(t.Name) + " *"
	}
	fatal("cannot map type", t)
	return ""
}

// decl renders a declaration of name with the given type.
// It exists so pointer declarators bind to the name without a stray space.
func (g *gen) decl(t *vegoc.Type, name string) string {
	s := g.typ(t)
	if strings.HasSuffix(s, "*") {
		return s + name
	}
	return s + " " + name
}

func (g *gen) collect() {
	g.sliceTypes = map[string]*vegoc.Type{}
	g.arrayTypes = map[string]*vegoc.Type{}
	var addType func(t *vegoc.Type)
	addType = func(t *vegoc.Type) {
		if t == nil {
			return
		}
		switch t.K {
		case vegoc.KSlice:
			addType(t.Elem)
			key := g.mangle(t)
			if _, ok := g.sliceTypes[key]; !ok {
				g.sliceTypes[key] = t
				g.sliceOrder = append(g.sliceOrder, key)
			}
		case vegoc.KArray:
			addType(t.Elem)
			key := g.mangle(t)
			if _, ok := g.arrayTypes[key]; !ok {
				g.arrayTypes[key] = t
				g.arrayOrder = append(g.arrayOrder, key)
			}
		case vegoc.KTuple:
			addType(t.Tup[0])
			addType(t.Tup[1])
		}
	}
	fe := func(e *vegoc.Expr) {
		addType(e.Typ)
		addType(e.TypeRef)
	}
	fs := func(s *vegoc.Stmt) {
		addType(s.TypeRef)
		for _, t := range s.DeclaredTypes {
			addType(t)
		}
	}
	for _, d := range g.p.Consts {
		addType(d.Inferred)
	}
	for _, d := range g.p.Vars {
		addType(d.Inferred)
	}
	for _, s := range g.p.Types {
		for _, f := range s.Fields {
			addType(f.Type)
		}
	}
	for _, f := range g.p.Funcs {
		for _, pa := range f.Params {
			addType(pa.Type)
		}
		for _, r := range f.Results {
			addType(r)
		}
		vegoc.WalkBody(f.Body, fe, fs)
	}
}

func (g *gen) files() (string, string) {
	g.collect()
	tups := map[string][2]*vegoc.Type{}
	for _, f := range g.p.Funcs {
		if len(f.Results) == 2 {
			pair := [2]*vegoc.Type{f.Results[0], f.Results[1]}
			tups[vegoc.TupName(pair)] = pair
		}
	}

	var h strings.Builder
	guard := strings.ToUpper(g.prefix) + "_" + strings.ToUpper(strings.NewReplacer(".", "_", "-", "_").Replace(g.hdrName))
	h.WriteString("// Code generated by json2c from the Vego JSON form. DO NOT EDIT.\n\n")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n#include \"vg.h\"\n\n", guard, guard)

	for _, d := range g.p.Consts {
		fmt.Fprintf(&h, "#define %s ((%s)(%s))\n", g.global(d.Name), g.typ(d.Inferred), g.expr(d.Value))
	}
	h.WriteString("\n")

	for _, s := range g.p.Types {
		fmt.Fprintf(&h, "typedef struct %s %s;\n", g.global(s.Name), g.global(s.Name))
	}
	for _, key := range g.arrayOrder {
		name := g.arrName(g.arrayTypes[key])
		fmt.Fprintf(&h, "typedef struct %s %s;\n", name, name)
	}
	h.WriteString("\n")

	for _, key := range g.sliceOrder {
		t := g.sliceTypes[key]
		fmt.Fprintf(&h, "typedef struct {\n    %s;\n    int64_t len;\n    int64_t cap;\n} %s;\n\n",
			g.declPtr(t.Elem, "p"), g.sliceName(t))
	}

	g.emitTypeDefs(&h)

	var names []string
	for n := range tups {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		pair := tups[n]
		fmt.Fprintf(&h, "typedef struct {\n    %s;\n    %s;\n} %s;\n\n",
			g.decl(pair[0], "r0"), g.decl(pair[1], "r1"), g.global(n))
	}

	for _, d := range g.p.Vars {
		fmt.Fprintf(&h, "extern const %s;\n", g.decl(d.Inferred, g.global(d.Name)))
	}
	if len(g.p.Vars) > 0 {
		h.WriteString("\n")
	}

	g.emitEqHelpers(&h)
	g.emitSliceHelpers(&h)
	g.emitArrayHelpers(&h)

	for _, f := range g.p.Funcs {
		fmt.Fprintf(&h, "%s;\n", g.signature(f))
	}
	fmt.Fprintf(&h, "\n#endif\n")

	g.b.Reset()
	g.wf("// Code generated by json2c from the Vego JSON form. DO NOT EDIT.\n\n")
	g.wf("#include \"%s\"\n\n", g.hdrName)
	for _, d := range g.p.Vars {
		g.wf("const %s = %s;\n\n", g.decl(d.Inferred, g.global(d.Name)), g.staticInit(d.Value))
	}
	for _, f := range g.p.Funcs {
		g.emitFunc(f)
	}
	return h.String(), g.b.String()
}

// declPtr renders a pointer declaration to the element type.
func (g *gen) declPtr(t *vegoc.Type, name string) string {
	s := g.typ(t)
	if strings.HasSuffix(s, "*") {
		return s + "*" + name
	}
	return s + " *" + name
}

// emitTypeDefs emits struct and array wrapper definitions.
// A struct comes after every struct or array it embeds by value.
// Go accepts any order, but C does not.
func (g *gen) emitTypeDefs(h *strings.Builder) {
	state := map[string]int{}
	var visitType func(t *vegoc.Type)
	var visitStruct func(s *vegoc.StructDecl)
	visitArray := func(t *vegoc.Type) {}
	visitType = func(t *vegoc.Type) {
		switch t.K {
		case vegoc.KStruct:
			visitStruct(g.p.StructMap[t.Name])
		case vegoc.KArray:
			visitArray(t)
		}
	}
	visitStruct = func(s *vegoc.StructDecl) {
		key := "struct " + s.Name
		switch state[key] {
		case 2:
			return
		case 1:
			fatal("struct value cycle at", s.Name)
		}
		state[key] = 1
		for _, f := range s.Fields {
			visitType(f.Type)
		}
		state[key] = 2
		fmt.Fprintf(h, "struct %s {\n", g.global(s.Name))
		if len(s.Fields) == 0 {
			// C11 forbids an empty struct.
			h.WriteString("    char vego_empty;\n")
		}
		for _, f := range s.Fields {
			fmt.Fprintf(h, "    %s;\n", g.decl(f.Type, ident(f.Name)))
		}
		h.WriteString("};\n\n")
	}
	visitArray = func(t *vegoc.Type) {
		key := "array " + g.mangle(t)
		switch state[key] {
		case 2:
			return
		case 1:
			fatal("array value cycle at", g.mangle(t))
		}
		state[key] = 1
		visitType(t.Elem)
		state[key] = 2
		n := t.ALenVal
		if n < 1 {
			// C11 forbids a zero-length array.
			// The dummy element is never read, because the resolved length drives every loop and bound.
			n = 1
		}
		fmt.Fprintf(h, "struct %s {\n    %s;\n};\n\n", g.arrName(t), g.declArr(t.Elem, "v", n))
	}
	for _, s := range g.p.Types {
		visitStruct(s)
	}
	for _, key := range g.arrayOrder {
		visitArray(g.arrayTypes[key])
	}
}

func (g *gen) declArr(elem *vegoc.Type, name string, n int64) string {
	return fmt.Sprintf("%s[%d]", g.decl(elem, name), n)
}

// comparable reports whether Go allows == on the type.
func (g *gen) comparable(t *vegoc.Type) bool {
	switch t.K {
	case vegoc.KSlice, vegoc.KPtr:
		return false
	case vegoc.KArray:
		return g.comparable(t.Elem)
	case vegoc.KStruct:
		for _, f := range g.p.StructMap[t.Name].Fields {
			if !g.comparable(f.Type) {
				return false
			}
		}
		return true
	}
	return true
}

// eqCall renders an equality test of two rendered values of type t.
func (g *gen) eqCall(t *vegoc.Type, a, b string) string {
	switch t.K {
	case vegoc.KStr:
		return fmt.Sprintf("vg_streq(%s, %s)", a, b)
	case vegoc.KStruct:
		return fmt.Sprintf("%s_eq(%s, %s)", g.global(t.Name), a, b)
	case vegoc.KArray:
		return fmt.Sprintf("%s_eq(%s, %s)", g.arrName(t), a, b)
	}
	return fmt.Sprintf("%s == %s", a, b)
}

// emitEqHelpers emits an equality function for every comparable struct and array type.
// Go compares these types by value, and C has no built-in aggregate comparison.
func (g *gen) emitEqHelpers(h *strings.Builder) {
	type eqDecl struct {
		name string
		t    *vegoc.Type
	}
	var decls []eqDecl
	for _, s := range g.p.Types {
		t := &vegoc.Type{K: vegoc.KStruct, Name: s.Name}
		if g.comparable(t) {
			decls = append(decls, eqDecl{g.global(s.Name), t})
		}
	}
	for _, key := range g.arrayOrder {
		t := g.arrayTypes[key]
		if g.comparable(t) {
			decls = append(decls, eqDecl{g.arrName(t), t})
		}
	}
	if len(decls) == 0 {
		return
	}
	for _, d := range decls {
		fmt.Fprintf(h, "static inline bool %s_eq(%s a, %s b);\n", d.name, g.typ(d.t), g.typ(d.t))
	}
	h.WriteString("\n")
	for _, d := range decls {
		fmt.Fprintf(h, "static inline bool %s_eq(%s a, %s b) {\n", d.name, g.typ(d.t), g.typ(d.t))
		if d.t.K == vegoc.KArray {
			if d.t.ALenVal == 0 {
				h.WriteString("    (void)a;\n    (void)b;\n    return true;\n}\n\n")
				continue
			}
			fmt.Fprintf(h, "    for (int64_t i = 0; i < %d; i++) {\n", d.t.ALenVal)
			fmt.Fprintf(h, "        if (!(%s)) {\n            return false;\n        }\n    }\n",
				g.eqCall(d.t.Elem, "a.v[i]", "b.v[i]"))
			h.WriteString("    return true;\n}\n\n")
			continue
		}
		fields := g.p.StructMap[d.t.Name].Fields
		if len(fields) == 0 {
			h.WriteString("    (void)a;\n    (void)b;\n    return true;\n}\n\n")
			continue
		}
		var parts []string
		for _, f := range fields {
			parts = append(parts, g.eqCall(f.Type, "a."+ident(f.Name), "b."+ident(f.Name)))
		}
		fmt.Fprintf(h, "    return %s;\n}\n\n", strings.Join(parts, " && "))
	}
}

// emitSliceHelpers emits the slice machinery, one family per element type.
// The semantics mirror vg.hpp of the C++ target: zeroed allocations, doubling growth with a zeroed spare region, and asserted bounds.
func (g *gen) emitSliceHelpers(h *strings.Builder) {
	for _, key := range g.sliceOrder {
		t := g.sliceTypes[key]
		s := g.sliceName(t)
		e := g.typ(t.Elem)
		ep := g.declPtr(t.Elem, "")
		ep = strings.TrimSuffix(ep, " ")
		fmt.Fprintf(h, "static inline %s %s_make_cap(vg_arena *mem, int64_t n, int64_t c) {\n", s, s)
		fmt.Fprintf(h, "    assert(0 <= n && n <= c);\n")
		fmt.Fprintf(h, "    %sp = (%s)vg_arena_alloc(mem, (size_t)(c < 1 ? 1 : c) * sizeof(%s));\n", g.declPtr(t.Elem, ""), ep, e)
		fmt.Fprintf(h, "    memset(p, 0, (size_t)c * sizeof(%s));\n", e)
		fmt.Fprintf(h, "    return (%s){p, n, c};\n}\n\n", s)
		fmt.Fprintf(h, "static inline %s %s_make(vg_arena *mem, int64_t n) {\n    return %s_make_cap(mem, n, n);\n}\n\n", s, s, s)
		fmt.Fprintf(h, "static inline %s %s_grow(vg_arena *mem, %s s, int64_t need) {\n", s, s, s)
		fmt.Fprintf(h, "    int64_t newcap = s.cap * 2 < 8 ? 8 : s.cap * 2;\n")
		fmt.Fprintf(h, "    if (newcap < need) {\n        newcap = need;\n    }\n")
		fmt.Fprintf(h, "    %sp = (%s)vg_arena_alloc(mem, (size_t)newcap * sizeof(%s));\n", g.declPtr(t.Elem, ""), ep, e)
		fmt.Fprintf(h, "    memset(p + s.len, 0, (size_t)(newcap - s.len) * sizeof(%s));\n", e)
		fmt.Fprintf(h, "    if (s.p != NULL && s.len > 0) {\n        memcpy(p, s.p, (size_t)s.len * sizeof(%s));\n    }\n", e)
		fmt.Fprintf(h, "    return (%s){p, s.len, newcap};\n}\n\n", s)
		fmt.Fprintf(h, "static inline %s %s_append(vg_arena *mem, %s s, %s v) {\n", s, s, s, e)
		fmt.Fprintf(h, "    if (s.len == s.cap) {\n        s = %s_grow(mem, s, s.len + 1);\n    }\n", s)
		fmt.Fprintf(h, "    s.p[s.len] = v;\n    s.len++;\n    return s;\n}\n\n")
		fmt.Fprintf(h, "static inline %s %s_append_slice(vg_arena *mem, %s s, %s more) {\n", s, s, s, s)
		fmt.Fprintf(h, "    if (s.len + more.len > s.cap) {\n        s = %s_grow(mem, s, s.len + more.len);\n    }\n", s)
		fmt.Fprintf(h, "    if (more.len > 0) {\n        memmove(s.p + s.len, more.p, (size_t)more.len * sizeof(%s));\n    }\n", e)
		fmt.Fprintf(h, "    s.len += more.len;\n    return s;\n}\n\n")
		fmt.Fprintf(h, "static inline %s %s_sub(%s s, int64_t lo, int64_t hi) {\n", s, s, s)
		fmt.Fprintf(h, "    assert(0 <= lo && lo <= hi && hi <= s.cap);\n")
		fmt.Fprintf(h, "    if (s.p == NULL) {\n        return (%s){0};\n    }\n", s)
		fmt.Fprintf(h, "    return (%s){s.p + lo, hi - lo, s.cap - lo};\n}\n\n", s)
		fmt.Fprintf(h, "static inline %s %s_tail(%s s, int64_t lo) {\n    return %s_sub(s, lo, s.len);\n}\n\n", s, s, s, s)
		fmt.Fprintf(h, "static inline %s %s_head(%s s, int64_t hi) {\n    return %s_sub(s, 0, hi);\n}\n\n", s, s, s, s)
		fmt.Fprintf(h, "static inline %s%s_at(%s s, int64_t i) {\n", g.declPtr(t.Elem, ""), s, s)
		fmt.Fprintf(h, "    assert(i >= 0 && i < s.len);\n    return &s.p[i];\n}\n\n")
		fmt.Fprintf(h, "static inline int64_t %s_copy(%s dst, %s src) {\n", s, s, s)
		fmt.Fprintf(h, "    int64_t n = dst.len < src.len ? dst.len : src.len;\n")
		fmt.Fprintf(h, "    if (n > 0) {\n        memmove(dst.p, src.p, (size_t)n * sizeof(%s));\n    }\n    return n;\n}\n\n", e)
		fmt.Fprintf(h, "static inline %s %s_of(vg_arena *mem, const %s*elems, int64_t n) {\n", s, s, strings.TrimSuffix(ep, "*"))
		fmt.Fprintf(h, "    %s out = %s_make(mem, n);\n", s, s)
		fmt.Fprintf(h, "    if (n > 0) {\n        memcpy(out.p, elems, (size_t)n * sizeof(%s));\n    }\n    return out;\n}\n\n", e)
		if t.Elem.K == vegoc.KU8 {
			g.emitByteBridges(h, s)
		}
	}
}

// emitByteBridges emits the conversions between vg_str and the byte slice.
func (g *gen) emitByteBridges(h *strings.Builder, s string) {
	fmt.Fprintf(h, "static inline %s %s_bytes_from_str(vg_arena *mem, vg_str v) {\n", s, g.prefix)
	fmt.Fprintf(h, "    %s out = %s_make(mem, v.len);\n", s, s)
	fmt.Fprintf(h, "    if (v.len > 0) {\n        memcpy(out.p, v.p, (size_t)v.len);\n    }\n    return out;\n}\n\n")
	fmt.Fprintf(h, "static inline vg_str %s_str_from_bytes(vg_arena *mem, %s b) {\n", g.prefix, s)
	fmt.Fprintf(h, "    char *p = (char *)vg_arena_alloc(mem, (size_t)(b.len < 1 ? 1 : b.len));\n")
	fmt.Fprintf(h, "    if (b.len > 0) {\n        memcpy(p, b.p, (size_t)b.len);\n    }\n")
	fmt.Fprintf(h, "    return (vg_str){p, b.len};\n}\n\n")
	fmt.Fprintf(h, "static inline %s %s_append_str(vg_arena *mem, %s s, vg_str more) {\n", s, s, s)
	fmt.Fprintf(h, "    if (s.len + more.len > s.cap) {\n        s = %s_grow(mem, s, s.len + more.len);\n    }\n", s)
	fmt.Fprintf(h, "    if (more.len > 0) {\n        memmove(s.p + s.len, more.p, (size_t)more.len);\n    }\n")
	fmt.Fprintf(h, "    s.len += more.len;\n    return s;\n}\n\n")
	fmt.Fprintf(h, "static inline int64_t %s_copy_str(%s dst, vg_str src) {\n", s, s)
	fmt.Fprintf(h, "    int64_t n = dst.len < src.len ? dst.len : src.len;\n")
	fmt.Fprintf(h, "    if (n > 0) {\n        memmove(dst.p, src.p, (size_t)n);\n    }\n    return n;\n}\n\n")
}

// emitArrayHelpers emits the array slicing helper for every array type whose element slice type exists.
func (g *gen) emitArrayHelpers(h *strings.Builder) {
	for _, key := range g.arrayOrder {
		t := g.arrayTypes[key]
		st := &vegoc.Type{K: vegoc.KSlice, Elem: t.Elem}
		if _, ok := g.sliceTypes[g.mangle(st)]; !ok {
			continue
		}
		a := g.arrName(t)
		s := g.sliceName(st)
		fmt.Fprintf(h, "static inline %s %s_slice(%s *a, int64_t lo, int64_t hi) {\n", s, a, a)
		fmt.Fprintf(h, "    assert(0 <= lo && lo <= hi && hi <= %d);\n", t.ALenVal)
		if t.ALenVal == 0 {
			// A Go slice of a zero-length array is non-nil, so the pointer addresses the array object itself.
			// With cap 0, nothing ever dereferences it.
			fmt.Fprintf(h, "    return (%s){(%s)(void *)a, 0, 0};\n}\n\n", s, strings.TrimSuffix(g.declPtr(t.Elem, ""), " "))
			continue
		}
		fmt.Fprintf(h, "    return (%s){a->v + lo, hi - lo, %d - lo};\n}\n\n", s, t.ALenVal)
	}
}

func (g *gen) signature(f *vegoc.FuncDecl) string {
	var params []string
	if f.Allocates {
		params = append(params, "vg_arena *mem")
	}
	for _, pa := range f.Params {
		params = append(params, g.decl(pa.Type, ident(pa.Name)))
	}
	if len(params) == 0 {
		params = append(params, "void")
	}
	ret := "void"
	switch len(f.Results) {
	case 1:
		ret = g.typ(f.Results[0])
	case 2:
		ret = g.global(vegoc.TupName([2]*vegoc.Type{f.Results[0], f.Results[1]}))
	}
	sep := " "
	if strings.HasSuffix(ret, "*") {
		sep = ""
	}
	return fmt.Sprintf("%s%s%s(%s)", ret, sep, g.global(f.Name), strings.Join(params, ", "))
}

func (g *gen) emitFunc(f *vegoc.FuncDecl) {
	g.fn = f
	g.resetNames(f)
	g.wf("%s {\n", g.signature(f))
	for _, pa := range f.Params {
		if info := f.Info[pa.Name]; info != nil && !info.Used {
			g.wf("    (void)%s;\n", ident(pa.Name))
		}
	}
	g.body(f.Body, 1)
	g.wf("}\n\n")
	g.fn = nil
}

func (g *gen) wf(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
}

func (g *gen) indent(depth int) {
	for range depth {
		g.b.WriteString("    ")
	}
}

func (g *gen) body(body []*vegoc.Stmt, depth int) {
	for _, s := range body {
		g.stmt(s, depth)
	}
}

func (g *gen) newTmp() string {
	for {
		g.tmp++
		name := fmt.Sprintf("_t%d", g.tmp)
		if !g.used[name] {
			g.used[name] = true
			return name
		}
	}
}

func (g *gen) resetNames(f *vegoc.FuncDecl) {
	g.tmp = 0
	g.used = map[string]bool{}
	if f != nil {
		for name := range f.Info {
			g.used[ident(name)] = true
		}
	}
}

// pushPre appends one prelude statement, indented for the statement under construction.
func (g *gen) pushPre(line string) {
	g.pre = append(g.pre, strings.Repeat("    ", g.depth)+line)
}

func (g *gen) flushPre() {
	for _, l := range g.pre {
		g.b.WriteString(l)
		g.b.WriteString("\n")
	}
	g.pre = nil
}

type checkpoint struct {
	pre  int
	tmp  int
	used map[string]bool
}

func (g *gen) save() checkpoint {
	used := make(map[string]bool, len(g.used))
	for k, v := range g.used {
		used[k] = v
	}
	return checkpoint{pre: len(g.pre), tmp: g.tmp, used: used}
}

func (g *gen) rollback(c checkpoint) {
	g.pre = g.pre[:c.pre]
	g.tmp = c.tmp
	g.used = c.used
}

// hoist pins a value into a fresh named temporary in the prelude and returns its name.
func (g *gen) hoist(t *vegoc.Type, value string) string {
	tmp := g.newTmp()
	g.pushPre(fmt.Sprintf("%s = %s;", g.decl(t, tmp), value))
	return tmp
}

func (g *gen) stmt(s *vegoc.Stmt, depth int) {
	g.depth = depth
	switch s.K {
	case "var_decl", "define", "assign", "op_assign":
		g.simpleStmt(s, depth)
	case "if":
		cond := g.expr(s.Cond)
		g.flushPre()
		g.indent(depth)
		g.wf("if (%s) {\n", cond)
		g.body(s.Then, depth+1)
		g.indent(depth)
		if s.HasElse {
			g.wf("} else {\n")
			g.body(s.Else, depth+1)
			g.indent(depth)
		}
		g.wf("}\n")
	case "for":
		g.emitFor(s, depth)
	case "range":
		g.emitRange(s, depth)
	case "switch":
		tag := g.expr(s.Tag)
		g.flushPre()
		g.indent(depth)
		g.wf("switch (%s) {\n", tag)
		for _, cs := range s.Cases {
			g.indent(depth)
			for i, v := range cs.Values {
				if i > 0 {
					g.wf(" ")
				}
				g.wf("case %s:", g.expr(v))
			}
			g.wf(" {\n")
			g.body(cs.Body, depth+1)
			g.indent(depth)
			g.wf("} break;\n")
		}
		g.indent(depth)
		if s.HasDef {
			g.wf("default: {\n")
			g.body(s.Default, depth+1)
			g.indent(depth)
			g.wf("} break;\n")
		} else {
			g.wf("default: break;\n")
		}
		g.indent(depth)
		g.wf("}\n")
	case "break":
		g.indent(depth)
		g.wf("break;\n")
	case "continue":
		g.indent(depth)
		g.wf("continue;\n")
	case "return":
		switch len(s.Values) {
		case 0:
			g.indent(depth)
			g.wf("return;\n")
		case 1:
			v := g.expr(s.Values[0])
			g.flushPre()
			g.indent(depth)
			g.wf("return %s;\n", v)
		default:
			values := g.renderInits(s.Values)
			g.flushPre()
			ret := g.global(vegoc.TupName([2]*vegoc.Type{g.fn.Results[0], g.fn.Results[1]}))
			g.indent(depth)
			g.wf("return (%s){%s, %s};\n", ret, values[0], values[1])
		}
	case "expr_stmt":
		v := g.expr(s.Value)
		g.flushPre()
		g.indent(depth)
		if s.Value.Typ != nil {
			g.wf("(void)(%s);\n", v)
		} else {
			g.wf("%s;\n", v)
		}
	case "block":
		g.indent(depth)
		g.wf("{\n")
		g.body(s.Body, depth+1)
		g.indent(depth)
		g.wf("}\n")
	default:
		fatal("unknown statement kind", s.K)
	}
}

// simpleStmt emits a declaration or assignment as full lines at the given depth.
func (g *gen) simpleStmt(s *vegoc.Stmt, depth int) {
	switch s.K {
	case "var_decl":
		if s.Value != nil {
			v := g.expr(s.Value)
			g.flushPre()
			g.indent(depth)
			g.wf("%s = %s;\n", g.decl(s.TypeRef, ident(s.Name)), v)
		} else {
			g.flushPre()
			g.indent(depth)
			g.wf("%s = {0};\n", g.decl(s.TypeRef, ident(s.Name)))
		}
	case "define":
		if len(s.Names) == 1 {
			v := g.expr(s.Value)
			g.flushPre()
			g.indent(depth)
			g.wf("%s = %s;\n", g.decl(s.DeclaredTypes[0], ident(s.Names[0])), v)
			return
		}
		v := g.expr(s.Value)
		g.flushPre()
		tup := g.tupleOf(s.Value)
		tmp := g.newTmp()
		g.indent(depth)
		g.wf("%s %s = %s;\n", tup, tmp, v)
		for i, n := range s.Names {
			if n == "_" {
				continue
			}
			g.indent(depth)
			g.wf("%s = %s.r%d;\n", g.decl(s.DeclaredTypes[i], ident(n)), tmp, i)
		}
	case "assign":
		if len(s.Lhs) == 2 {
			g.emitAssign2(s, depth)
			return
		}
		l := s.Lhs[0]
		if l.K == "ident" && l.Name == "_" {
			v := g.expr(s.Value)
			g.flushPre()
			g.indent(depth)
			g.wf("(void)(%s);\n", v)
			return
		}
		if vegoc.Impure(l) {
			// Go evaluates the target place before the value.
			place := g.pinPlace(l)
			v := g.expr(s.Value)
			g.flushPre()
			g.indent(depth)
			g.wf("%s = %s;\n", place, v)
			return
		}
		lstr := g.expr(l)
		v := g.expr(s.Value)
		g.flushPre()
		g.indent(depth)
		g.wf("%s = %s;\n", lstr, v)
	case "op_assign":
		place := ""
		if vegoc.Impure(s.Lhs[0]) {
			place = g.pinPlace(s.Lhs[0])
		} else {
			place = g.expr(s.Lhs[0])
		}
		v := g.expr(s.Value)
		g.flushPre()
		g.indent(depth)
		g.wf("%s;\n", g.opAssign(s, place, v))
	default:
		fatal("not a simple statement", s.K)
	}
}

// tupleOf names the tuple struct a two-result call returns.
func (g *gen) tupleOf(e *vegoc.Expr) string {
	if e.Typ == nil || e.Typ.K != vegoc.KTuple {
		fatal("expression is not a two-result call")
	}
	return g.global(vegoc.TupName(e.Typ.Tup))
}

// pinPlace evaluates an assignment target once, into a pointer temporary, and returns the dereference.
func (g *gen) pinPlace(l *vegoc.Expr) string {
	lstr := g.expr(l)
	tmp := g.newTmp()
	g.pushPre(fmt.Sprintf("%s = &(%s);", g.declPtr(l.Typ, tmp), lstr))
	return "(*" + tmp + ")"
}

func (g *gen) emitAssign2(s *vegoc.Stmt, depth int) {
	impure := false
	for _, l := range s.Lhs {
		if !(l.K == "ident" && l.Name == "_") && vegoc.Impure(l) {
			impure = true
		}
	}
	places := make([]string, len(s.Lhs))
	for i, l := range s.Lhs {
		if l.K == "ident" && l.Name == "_" {
			continue
		}
		if impure {
			places[i] = g.pinPlace(l)
		} else {
			places[i] = g.expr(l)
		}
	}
	v := g.expr(s.Value)
	g.flushPre()
	tup := g.tupleOf(s.Value)
	tmp := g.newTmp()
	g.indent(depth)
	g.wf("%s %s = %s;\n", tup, tmp, v)
	for i, l := range s.Lhs {
		if l.K == "ident" && l.Name == "_" {
			continue
		}
		g.indent(depth)
		g.wf("%s = %s.r%d;\n", places[i], tmp, i)
	}
}

// opAssign renders a compound assignment against a place that is already rendered.
// C has no &^=, signed division overflow stays undefined even under -fwrapv, and a signed left shift can overflow.
// Those forms therefore need a rewrite.
func (g *gen) opAssign(s *vegoc.Stmt, place, val string) string {
	t := s.Lhs[0].Typ
	switch s.Op {
	case "&^=":
		return fmt.Sprintf("%s &= ~(%s)", place, val)
	case "/=":
		if t.Signed() {
			return fmt.Sprintf("%s = vg_sdiv_%s(%s, %s)", place, g.mangle(t), place, val)
		}
	case "%=":
		if t.Signed() {
			return fmt.Sprintf("%s = vg_srem_%s(%s, %s)", place, g.mangle(t), place, val)
		}
	case "<<=":
		if t.Signed() || t.Width() < 32 {
			return fmt.Sprintf("%s = %s", place, g.shiftLeft(t, place, val))
		}
	}
	return fmt.Sprintf("%s %s %s", place, s.Op, val)
}

// shiftLeft renders a Go left shift through unsigned arithmetic.
// A signed left shift can overflow, which C11 leaves undefined.
// A narrow operand promotes to int, which is signed, so it takes the same route.
func (g *gen) shiftLeft(t *vegoc.Type, x, y string) string {
	var u string
	switch t.Width() {
	case 64:
		u = "uint64_t"
	default:
		u = "uint32_t"
	}
	return fmt.Sprintf("(%s)((%s)(%s) << (%s))", g.typ(t), u, x, y)
}

// emitFor lowers a Go for statement.
// An init statement gets its own wrapping block, so its prelude and scope stay local.
// A condition that needs a prelude moves to the top of the body, in front of a break.
// The post statement stays in the loop header, so continue keeps its meaning.
func (g *gen) emitFor(s *vegoc.Stmt, depth int) {
	if s.Init != nil {
		g.indent(depth)
		g.wf("{\n")
		g.stmt(s.Init, depth+1)
		inner := *s
		inner.Init = nil
		g.emitFor(&inner, depth+1)
		g.indent(depth)
		g.wf("}\n")
		return
	}
	post := ""
	if s.Post != nil {
		cp := g.save()
		g.depth = depth
		post = g.inlineStmt(s.Post)
		if len(g.pre) > cp.pre {
			fatal("loop post statement needs a temporary")
		}
	}
	if s.Cond == nil {
		g.indent(depth)
		if post == "" {
			g.wf("for (;;) {\n")
		} else {
			g.wf("for (;; %s) {\n", post)
		}
		g.body(s.Body, depth+1)
		g.indent(depth)
		g.wf("}\n")
		return
	}
	cp := g.save()
	g.depth = depth + 1
	cond := g.expr(s.Cond)
	if len(g.pre) == cp.pre {
		g.indent(depth)
		if post == "" {
			g.wf("while (%s) {\n", cond)
		} else {
			g.wf("for (; %s; %s) {\n", cond, post)
		}
		g.body(s.Body, depth+1)
		g.indent(depth)
		g.wf("}\n")
		return
	}
	g.indent(depth)
	if post == "" {
		g.wf("for (;;) {\n")
	} else {
		g.wf("for (;; %s) {\n", post)
	}
	g.flushPre()
	g.indent(depth + 1)
	g.wf("if (!(%s)) {\n", cond)
	g.indent(depth + 2)
	g.wf("break;\n")
	g.indent(depth + 1)
	g.wf("}\n")
	g.body(s.Body, depth+1)
	g.indent(depth)
	g.wf("}\n")
}

// inlineStmt renders a loop post statement, with no indentation and no terminator.
// Assignments are expressions in C, so the rendered forms fit a loop header.
func (g *gen) inlineStmt(s *vegoc.Stmt) string {
	switch s.K {
	case "assign":
		if len(s.Lhs) != 1 {
			fatal("two-value assign in loop header")
		}
		if vegoc.Impure(s.Lhs[0]) {
			fatal("impure loop header assignment needs a temporary")
		}
		return fmt.Sprintf("%s = %s", g.expr(s.Lhs[0]), g.expr(s.Value))
	case "op_assign":
		if vegoc.Impure(s.Lhs[0]) {
			fatal("impure loop header assignment needs a temporary")
		}
		return g.opAssign(s, g.expr(s.Lhs[0]), g.expr(s.Value))
	case "expr_stmt":
		return g.expr(s.Value)
	}
	fatal("unsupported loop header statement", s.K)
	return ""
}

// emitRange lowers a range statement to an index loop.
// The operand evaluates once into a copy, and a hidden counter drives the loop.
func (g *gen) emitRange(s *vegoc.Stmt, depth int) {
	g.depth = depth + 1
	g.indent(depth)
	g.wf("{\n")
	over := g.expr(s.Value)
	g.flushPre()
	overTmp := g.newTmp()
	g.indent(depth + 1)
	g.wf("%s = %s;\n", g.decl(s.Value.Typ, overTmp), over)
	limit := overTmp
	switch s.Value.Typ.K {
	case vegoc.KSlice:
		limit = overTmp + ".len"
	case vegoc.KArray:
		limit = fmt.Sprintf("%d", s.Value.Typ.ALenVal)
	}
	counter := g.newTmp()
	g.indent(depth + 1)
	g.wf("for (int64_t %s = 0; %s < %s; %s++) {\n", counter, counter, limit, counter)
	if s.IdxName != "" && s.IdxName != "_" {
		g.indent(depth + 2)
		g.wf("int64_t %s = %s;\n", ident(s.IdxName), counter)
	}
	if s.ValName != "" && s.ValName != "_" {
		elem := s.Value.Typ.Elem
		access := ""
		switch s.Value.Typ.K {
		case vegoc.KSlice:
			access = fmt.Sprintf("(*%s_at(%s, %s))", g.sliceName(s.Value.Typ), overTmp, counter)
		case vegoc.KArray:
			access = fmt.Sprintf("%s.v[%s]", overTmp, counter)
		default:
			fatal("range value over", s.Value.Typ)
		}
		g.indent(depth + 2)
		g.wf("%s = %s;\n", g.decl(elem, ident(s.ValName)), access)
	}
	g.body(s.Body, depth+2)
	g.indent(depth + 1)
	g.wf("}\n")
	g.indent(depth)
	g.wf("}\n")
}

// narrow reports whether a type needs a cast back after arithmetic, to defeat C integer promotion.
func narrow(t *vegoc.Type) bool {
	switch t.K {
	case vegoc.KU8, vegoc.KU16, vegoc.KU32, vegoc.KI32:
		return true
	}
	return false
}

func isNil(e *vegoc.Expr) bool {
	return e.K == "ident" && e.Name == "nil"
}

func constish(e *vegoc.Expr) bool {
	return e.IsConst || isNil(e)
}

// addressable reports whether the expression names storage that outlives the full expression.
func addressable(e *vegoc.Expr) bool {
	switch e.K {
	case "ident":
		return true
	case "field":
		if e.X.Typ.K == vegoc.KPtr {
			return true
		}
		return addressable(e.X)
	case "index", "slice_expr":
		if e.X.Typ.K == vegoc.KSlice || e.X.Typ.K == vegoc.KStr {
			return true
		}
		return addressable(e.X)
	}
	return false
}

func (g *gen) expr(e *vegoc.Expr) string {
	switch e.K {
	case "int", "char":
		return g.intLiteral(e)
	case "str":
		return "vg_lit(\"" + cEscape(e.Value) + "\")"
	case "bool":
		if e.BoolVal {
			return "true"
		}
		return "false"
	case "ident":
		if e.Name == "nil" {
			return "((" + g.typ(e.Typ) + "){0})"
		}
		if g.fn != nil {
			if _, local := g.fn.Info[e.Name]; local {
				return ident(e.Name)
			}
		}
		if g.isGlobal(e.Name) {
			return g.global(e.Name)
		}
		return ident(e.Name)
	case "field":
		sep := "."
		if e.X.Typ.K == vegoc.KPtr {
			sep = "->"
		}
		return g.expr(e.X) + sep + ident(e.Name)
	case "index":
		return g.index(e)
	case "slice_expr":
		return g.sliceExpr(e)
	case "call":
		args := g.renderArgs(e.Args)
		if g.p.CalleeAllocates(e.Name) {
			args = append([]string{"mem"}, args...)
		}
		return g.global(e.Name) + "(" + strings.Join(args, ", ") + ")"
	case "builtin":
		return g.builtin(e)
	case "conv":
		if e.TypeRef.K == vegoc.KStr {
			return g.prefix + "_str_from_bytes(mem, " + g.expr(e.X) + ")"
		}
		if e.TypeRef.K == vegoc.KSlice {
			return g.prefix + "_bytes_from_str(mem, " + g.expr(e.X) + ")"
		}
		return "((" + g.typ(e.TypeRef) + ")(" + g.expr(e.X) + "))"
	case "unary":
		switch e.Op {
		case "-":
			// Unsigned arithmetic keeps negation defined, and it wraps like Go, even for the minimum value.
			if e.Typ.IsInteger() {
				return fmt.Sprintf("((%s)(0ULL - (uint64_t)(%s)))", g.typ(e.Typ), g.expr(e.X))
			}
			return "(-" + g.expr(e.X) + ")"
		case "^":
			if narrow(e.Typ) {
				return "((" + g.typ(e.Typ) + ")~(" + g.expr(e.X) + "))"
			}
			return "(~(" + g.expr(e.X) + "))"
		case "!":
			return "(!(" + g.expr(e.X) + "))"
		case "&":
			fatal("& outside a call argument")
		}
		fatal("unknown unary", e.Op)
	case "binary":
		return g.binary(e)
	case "composite":
		return g.composite(e)
	}
	fatal("unknown expression kind", e.K)
	return ""
}

func (g *gen) isGlobal(name string) bool {
	if _, ok := g.p.ConstMap[name]; ok {
		return true
	}
	if _, ok := g.p.VarMap[name]; ok {
		return true
	}
	_, ok := g.p.FuncMap[name]
	return ok
}

// index renders an index expression.
// C leaves the base and the index unsequenced, so the printer pins them in Go's order when a side effect can show the difference.
func (g *gen) index(e *vegoc.Expr) string {
	base := ""
	idx := ""
	arrayByPtr := false
	if (vegoc.Impure(e.X) && !constish(e.Index)) || (vegoc.Impure(e.Index) && !constish(e.X)) {
		if e.X.Typ.K == vegoc.KArray && addressable(e.X) {
			tmp := g.newTmp()
			g.pushPre(fmt.Sprintf("%s = &(%s);", g.declPtr(e.X.Typ, tmp), g.expr(e.X)))
			base = tmp
			arrayByPtr = true
		} else {
			base = g.hoist(e.X.Typ, g.expr(e.X))
		}
		idx = g.hoist(e.Index.Typ, g.expr(e.Index))
	} else {
		base = g.expr(e.X)
		idx = g.expr(e.Index)
	}
	switch e.X.Typ.K {
	case vegoc.KSlice:
		return fmt.Sprintf("(*%s_at(%s, %s))", g.sliceName(e.X.Typ), base, idx)
	case vegoc.KStr:
		return fmt.Sprintf("vg_str_at(%s, %s)", base, idx)
	case vegoc.KArray:
		if arrayByPtr {
			return fmt.Sprintf("%s->v[%s]", base, idx)
		}
		return fmt.Sprintf("%s.v[%s]", base, idx)
	}
	fatal("index of", e.X.Typ)
	return ""
}

func (g *gen) sliceExpr(e *vegoc.Expr) string {
	hoisting := vegoc.Impure(e.X) || vegoc.Impure(e.Lo) || vegoc.Impure(e.Hi)
	arrayBase := ""
	x := ""
	if e.X.Typ.K == vegoc.KArray {
		// The array helper takes the base by pointer, so the base must name real storage.
		if addressable(e.X) {
			arrayBase = "&(" + g.expr(e.X) + ")"
			if hoisting {
				tmp := g.newTmp()
				g.pushPre(fmt.Sprintf("%s = %s;", g.declPtr(e.X.Typ, tmp), arrayBase))
				arrayBase = tmp
			}
		} else {
			tmp := g.hoist(e.X.Typ, g.expr(e.X))
			arrayBase = "&" + tmp
		}
	} else {
		x = g.expr(e.X)
		if hoisting {
			x = g.hoist(e.X.Typ, x)
		}
	}
	render := func(b *vegoc.Expr) string {
		s := g.expr(b)
		if hoisting {
			s = g.hoist(b.Typ, s)
		}
		return s
	}
	switch e.X.Typ.K {
	case vegoc.KSlice, vegoc.KStr:
		fn := "vg_str"
		if e.X.Typ.K == vegoc.KSlice {
			fn = g.sliceName(e.X.Typ)
		}
		switch {
		case e.Lo == nil && e.Hi == nil:
			return x
		case e.Lo == nil:
			return fmt.Sprintf("%s_head(%s, %s)", fn, x, render(e.Hi))
		case e.Hi == nil:
			return fmt.Sprintf("%s_tail(%s, %s)", fn, x, render(e.Lo))
		default:
			return fmt.Sprintf("%s_sub(%s, %s, %s)", fn, x, render(e.Lo), render(e.Hi))
		}
	case vegoc.KArray:
		lo := "0"
		if e.Lo != nil {
			lo = render(e.Lo)
		}
		hi := fmt.Sprintf("%d", e.X.Typ.ALenVal)
		if e.Hi != nil {
			hi = render(e.Hi)
		}
		return fmt.Sprintf("%s_slice(%s, %s, %s)", g.arrName(e.X.Typ), arrayBase, lo, hi)
	}
	fatal("slice of", e.X.Typ)
	return ""
}

func (g *gen) intLiteral(e *vegoc.Expr) string {
	v, err := strconv.ParseUint(e.Value, 10, 64)
	if err != nil {
		fatal("bad integer literal", e.Value)
	}
	if e.Typ != nil {
		switch e.Typ.K {
		case vegoc.KU64:
			return e.Value + "ULL"
		case vegoc.KI64, vegoc.KInt:
			// The magnitude of MinInt64 under unary minus has no signed spelling.
			// The conversion comes from unsigned instead, and the cast truncates modularly on every real target.
			if v > 1<<63-1 {
				return "((int64_t)" + e.Value + "ULL)"
			}
			return e.Value + "LL"
		case vegoc.KI32:
			if v > 1<<31-1 {
				return "((int32_t)" + e.Value + "U)"
			}
		}
	}
	return e.Value
}

func (g *gen) builtin(e *vegoc.Expr) string {
	switch e.Name {
	case "len":
		a := e.Args[0]
		switch a.Typ.K {
		case vegoc.KSlice, vegoc.KStr:
			return "(" + g.expr(a) + ").len"
		case vegoc.KArray:
			if vegoc.Impure(a) {
				g.pushPre(fmt.Sprintf("(void)(%s);", g.expr(a)))
			}
			return fmt.Sprintf("%dLL", a.Typ.ALenVal)
		}
		fatal("len of", a.Typ)
	case "cap":
		return "(" + g.expr(e.Args[0]) + ").cap"
	case "make":
		name := g.sliceName(e.TypeRef)
		args := g.renderArgs(e.Args)
		if len(args) == 2 {
			return fmt.Sprintf("%s_make_cap(mem, %s, %s)", name, args[0], args[1])
		}
		return fmt.Sprintf("%s_make(mem, %s)", name, args[0])
	case "append":
		name := g.sliceName(e.Args[0].Typ)
		args := g.renderArgs(e.Args)
		if e.Spread {
			if e.Args[1].Typ.K == vegoc.KStr {
				return fmt.Sprintf("%s_append_str(mem, %s, %s)", name, args[0], args[1])
			}
			return fmt.Sprintf("%s_append_slice(mem, %s, %s)", name, args[0], args[1])
		}
		out := args[0]
		for _, a := range args[1:] {
			out = fmt.Sprintf("%s_append(mem, %s, %s)", name, out, a)
		}
		return out
	case "copy":
		name := g.sliceName(e.Args[0].Typ)
		args := g.renderArgs(e.Args)
		if e.Args[1].Typ.K == vegoc.KStr {
			return fmt.Sprintf("%s_copy_str(%s, %s)", name, args[0], args[1])
		}
		return fmt.Sprintf("%s_copy(%s, %s)", name, args[0], args[1])
	case "min", "max":
		args := g.renderArgs(e.Args)
		return fmt.Sprintf("vg_%s_%s(%s, %s)", e.Name, g.mangle(e.Typ), args[0], args[1])
	}
	fatal("unknown builtin", e.Name)
	return ""
}

// renderArgs renders call and builtin arguments in Go's left-to-right order.
// When the unspecified argument order of C could show, every non-constant argument moves into a prelude temporary.
// A borrow argument, written &x, binds the same place whenever it evaluates, so it stays inline.
func (g *gen) renderArgs(args []*vegoc.Expr) []string {
	inlineArg := func(a *vegoc.Expr) bool {
		return (a.Typ != nil && a.Typ.K == vegoc.KPtr) || constish(a)
	}
	impure, others := 0, 0
	for _, a := range args {
		if inlineArg(a) {
			continue
		}
		if vegoc.Impure(a) {
			impure++
		} else {
			others++
		}
	}
	pin := impure > 0 && impure+others >= 2
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a.K == "unary" && a.Op == "&" {
			out = append(out, "&("+g.expr(a.X)+")")
			continue
		}
		s := g.expr(a)
		if pin && !inlineArg(a) {
			s = g.hoist(a.Typ, s)
		}
		out = append(out, s)
	}
	return out
}

func (g *gen) binary(e *vegoc.Expr) string {
	if e.Op == "&&" || e.Op == "||" {
		return g.logical(e)
	}
	// C leaves binary operands unsequenced, but Go evaluates left to right.
	// The printer therefore pins the operands into ordered temporaries when a side effect can show the difference.
	// The rebuilt expression dispatches again, so string and struct comparisons keep their helpers.
	if (vegoc.Impure(e.X) && !constish(e.Y)) || (vegoc.Impure(e.Y) && !constish(e.X)) {
		clone := *e
		hx := g.hoist(e.X.Typ, g.expr(e.X))
		hy := g.hoist(e.Y.Typ, g.expr(e.Y))
		clone.X = &vegoc.Expr{K: "ident", Name: hx, Typ: e.X.Typ}
		clone.Y = &vegoc.Expr{K: "ident", Name: hy, Typ: e.Y.Typ}
		return g.binary(&clone)
	}
	if e.Op == "==" || e.Op == "!=" {
		if isNil(e.Y) && e.X.Typ.K == vegoc.KSlice {
			return "((" + g.expr(e.X) + ").p " + e.Op + " NULL)"
		}
		if isNil(e.X) && e.Y.Typ.K == vegoc.KSlice {
			return "((" + g.expr(e.Y) + ").p " + e.Op + " NULL)"
		}
		if e.X.Typ.K == vegoc.KStruct || e.X.Typ.K == vegoc.KArray {
			eq := g.eqCall(e.X.Typ, g.expr(e.X), g.expr(e.Y))
			if e.Op == "!=" {
				return "(!" + eq + ")"
			}
			return "(" + eq + ")"
		}
	}
	if e.X.Typ.K == vegoc.KStr {
		x, y := g.expr(e.X), g.expr(e.Y)
		switch e.Op {
		case "==":
			return fmt.Sprintf("vg_streq(%s, %s)", x, y)
		case "!=":
			return fmt.Sprintf("(!vg_streq(%s, %s))", x, y)
		case "<", "<=", ">", ">=":
			return fmt.Sprintf("(vg_strcmp3(%s, %s) %s 0)", x, y, e.Op)
		}
	}
	x, y := g.expr(e.X), g.expr(e.Y)
	var body string
	switch e.Op {
	case "==", "!=", "<", "<=", ">", ">=":
		return fmt.Sprintf("(%s %s %s)", x, e.Op, y)
	case "/":
		if e.Typ.Signed() {
			// Go defines MinInt / -1 as MinInt, which C leaves undefined even under -fwrapv.
			return fmt.Sprintf("vg_sdiv_%s(%s, %s)", g.mangle(e.Typ), x, y)
		}
		body = fmt.Sprintf("%s / %s", x, y)
	case "%":
		if e.Typ.Signed() {
			return fmt.Sprintf("vg_srem_%s(%s, %s)", g.mangle(e.Typ), x, y)
		}
		body = fmt.Sprintf("%s %% %s", x, y)
	case "&^":
		body = fmt.Sprintf("%s & ~(%s)", x, y)
	case "<<":
		if e.Typ.Signed() || e.Typ.Width() < 32 {
			return g.shiftLeft(e.Typ, x, y)
		}
		body = fmt.Sprintf("%s << %s", x, y)
	case "+", "-", "*", "&", "|", "^", ">>":
		body = fmt.Sprintf("%s %s %s", x, e.Op, y)
	default:
		fatal("unknown binary op", e.Op)
	}
	if narrow(e.Typ) {
		return "((" + g.typ(e.Typ) + ")(" + body + "))"
	}
	return "(" + body + ")"
}

// logical renders && and ||.
// C sequences the operands, so the plain form is usually right.
// When the right operand needs prelude statements, their unconditional evaluation would be wrong.
// The expression then becomes a temporary that an if statement completes.
func (g *gen) logical(e *vegoc.Expr) string {
	x := g.expr(e.X)
	cp := g.save()
	y := g.expr(e.Y)
	if len(g.pre) == cp.pre {
		return fmt.Sprintf("(%s %s %s)", x, e.Op, y)
	}
	g.rollback(cp)
	tmp := g.newTmp()
	g.pushPre(fmt.Sprintf("bool %s = %s;", tmp, x))
	if e.Op == "&&" {
		g.pushPre(fmt.Sprintf("if (%s) {", tmp))
	} else {
		g.pushPre(fmt.Sprintf("if (!%s) {", tmp))
	}
	g.depth++
	y = g.expr(e.Y)
	g.pushPre(fmt.Sprintf("%s = %s;", tmp, y))
	g.depth--
	g.pushPre("}")
	return tmp
}

func (g *gen) composite(e *vegoc.Expr) string {
	t := e.TypeRef
	switch t.K {
	case vegoc.KStruct:
		if len(e.Fields) == 0 {
			return "((" + g.typ(t) + "){0})"
		}
		var vals []*vegoc.Expr
		for _, f := range e.Fields {
			vals = append(vals, f.Value)
		}
		rendered := g.renderInits(vals)
		var parts []string
		for i, f := range e.Fields {
			parts = append(parts, fmt.Sprintf(".%s = %s", ident(f.Name), rendered[i]))
		}
		return fmt.Sprintf("((%s){%s})", g.typ(t), strings.Join(parts, ", "))
	case vegoc.KSlice:
		if len(e.Elems) == 0 {
			return fmt.Sprintf("%s_make(mem, 0)", g.sliceName(t))
		}
		rendered := g.renderInits(e.Elems)
		return fmt.Sprintf("%s_of(mem, (%s[]){%s}, %d)",
			g.sliceName(t), g.typ(t.Elem), strings.Join(rendered, ", "), len(e.Elems))
	case vegoc.KArray:
		if len(e.Elems) == 0 {
			return "((" + g.typ(t) + "){0})"
		}
		rendered := g.renderInits(e.Elems)
		return fmt.Sprintf("((%s){.v = {%s}})", g.typ(t), strings.Join(rendered, ", "))
	}
	fatal("composite of", t)
	return ""
}

// renderInits renders initializer expressions in Go's left-to-right order.
// C leaves the evaluation order between initializers unspecified, so an initializer list with a visible side effect moves into prelude temporaries.
func (g *gen) renderInits(vals []*vegoc.Expr) []string {
	impure, others := 0, 0
	for _, v := range vals {
		if constish(v) {
			continue
		}
		if vegoc.Impure(v) {
			impure++
		} else {
			others++
		}
	}
	pin := impure > 0 && impure+others >= 2
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		s := g.expr(v)
		if pin && !constish(v) {
			s = g.hoist(v.Typ, s)
		}
		out = append(out, s)
	}
	return out
}

// staticInit renders a file-scope initializer.
// C11 allows no compound literals there, so aggregates use plain braced initializers.
func (g *gen) staticInit(e *vegoc.Expr) string {
	switch e.K {
	case "int", "char":
		return g.intLiteral(e)
	case "bool":
		if e.BoolVal {
			return "true"
		}
		return "false"
	case "str":
		return fmt.Sprintf("{\"%s\", %dLL}", cEscape(e.Value), len(e.Value))
	case "ident":
		if g.isGlobal(e.Name) {
			return g.global(e.Name)
		}
		fatal("static initializer references a local", e.Name)
	case "unary", "binary", "conv":
		return g.expr(e)
	case "composite":
		t := e.TypeRef
		switch t.K {
		case vegoc.KStruct:
			if len(e.Fields) == 0 {
				return "{0}"
			}
			var parts []string
			for _, f := range e.Fields {
				parts = append(parts, fmt.Sprintf(".%s = %s", ident(f.Name), g.staticInit(f.Value)))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		case vegoc.KArray:
			if len(e.Elems) == 0 {
				return "{0}"
			}
			var parts []string
			for _, el := range e.Elems {
				parts = append(parts, g.staticInit(el))
			}
			return "{.v = {" + strings.Join(parts, ", ") + "}}"
		}
		fatal("static initializer composite of", t)
	}
	fatal("unsupported static initializer kind", e.K)
	return ""
}

// cEscape escapes a string literal.
// A non-printable byte uses a three-digit octal escape, which has a fixed length.
// A question mark is escaped, so no trigraph sequence can form.
func cEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString("\\\"")
		case c == '\\':
			b.WriteString("\\\\")
		case c == '?':
			b.WriteString("\\?")
		case c < 32 || c > 126:
			fmt.Fprintf(&b, "\\%03o", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
