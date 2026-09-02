// Package ts prints the Vego JSON form as one TypeScript module.
// The output imports the hand-written runtime vg.ts.
// That runtime supplies the Slice and Str types, the buffer operations, and the integer helpers.
//
// The integer mapping is the one decision that shapes the output.
// A Vego int is 64 bits wide, and a JavaScript number is exact only up to 2^53.
// The printer maps int and the 32-bit types to number, and checks every 64-bit add, subtract, multiply and shift with vg.chk, which throws past 2^53.
// int64 and uint64 map to bigint, so they stay exact at every width.
// The 32-bit and narrower types wrap with the ToInt32 and ToUint32 idioms and Math.imul.
//
// Structs become classes with a clone method.
// Go copies a struct on assignment, and JavaScript shares it, so the printer clones at every site where a stored or returned value comes from a place expression.
// Slices become immutable headers over typed arrays or plain arrays, so sharing a header is as safe as copying a Go slice header.
//
// The functions take no memory context: the garbage collector owns every buffer.
package ts

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/oneregex/revera/vego/compiler"
)

// failure carries a printer error out of the recursive emitters.
type failure string

func fatal(args ...any) {
	panic(failure(strings.TrimSuffix(fmt.Sprintln(args...), "\n")))
}

// catch turns a failure raised by fatal into the error result of an Emit call.
// Any other panic is a bug and keeps propagating.
func catch(err *error) {
	if r := recover(); r != nil {
		f, ok := r.(failure)
		if !ok {
			panic(r)
		}
		*err = errors.New(string(f))
	}
}

// Emit prints the program as one TypeScript module.
func Emit(p *compiler.Program) (src string, err error) {
	defer catch(&err)
	g := &gen{p: p, lits: map[string]string{}}
	return g.file(), nil
}

type gen struct {
	p    *compiler.Program
	b    strings.Builder
	fn   *compiler.FuncDecl
	tmp  int
	used map[string]bool

	// temps lists the hoisted temporaries of the current function, with their types.
	temps []string
	// loops is the stack of enclosing loops; each entry records whether a break inside a switch needs its label.
	loops []*loopInfo
	// switchDepth counts the switch statements between the innermost loop and the current statement.
	switchDepth int

	// lits maps string literal bytes to the module-level constant that holds them.
	lits     map[string]string
	litOrder []string
}

type loopInfo struct {
	label   string
	labeled bool
}

// reserved lists the names that cannot be a JavaScript binding, plus the globals the output relies on.
var reserved = map[string]bool{
	"await": true, "break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "debugger": true, "default": true,
	"delete": true, "do": true, "else": true, "enum": true, "export": true,
	"extends": true, "false": true, "finally": true, "for": true,
	"function": true, "if": true, "import": true, "in": true,
	"instanceof": true, "new": true, "null": true, "return": true,
	"super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true,
	"with": true, "yield": true, "let": true, "static": true,
	"implements": true, "interface": true, "package": true, "private": true,
	"protected": true, "public": true, "arguments": true, "eval": true,
	"undefined": true, "NaN": true, "Infinity": true,
	"vg": true, "Math": true, "BigInt": true, "Number": true, "Array": true,
	"Uint8Array": true, "Uint16Array": true, "Uint32Array": true,
	"Int32Array": true, "Float64Array": true, "BigInt64Array": true,
	"BigUint64Array": true, "RangeError": true, "Object": true, "String": true,
	"Symbol": true, "clone": true, "eq": true, "elem": true, "constructor": true,
	"prototype": true, "any": true, "number": true, "string": true, "boolean": true,
	"bigint": true, "never": true, "unknown": true, "type": true, "declare": true,
	"readonly": true, "as": true, "is": true, "of": true, "get": true, "set": true,
}

// ident maps a Vego name to a TypeScript binding name.
// A reserved name gets an underscore suffix, and a name that already ends in an underscore gets a second one.
// The mapping is injective, so two program names never collide.
func ident(name string) string {
	if reserved[name] || strings.HasSuffix(name, "_") {
		return name + "_"
	}
	return name
}

// field maps a struct field name.
// A property can carry any name, but the class methods clone and eq, and the static elem, must stay free.
func field(name string) string {
	switch name {
	case "clone", "eq", "elem", "constructor", "prototype", "__proto__":
		return name + "_"
	}
	if strings.HasSuffix(name, "_") {
		return name + "_"
	}
	return name
}

func (g *gen) wf(format string, args ...any) {
	fmt.Fprintf(&g.b, format, args...)
}

func (g *gen) file() string {
	var body strings.Builder
	saved := g.b
	g.b = strings.Builder{}
	for _, d := range g.p.Consts {
		g.constDecl(d)
	}
	g.wf("\n")
	for _, d := range g.p.Vars {
		g.wf("export const %s: %s = %s;\n", ident(d.Name), g.typ(d.Inferred), g.expr(d.Value))
	}
	g.wf("\n")
	for _, s := range g.p.Types {
		g.structDecl(s)
	}
	for _, f := range g.p.Funcs {
		g.emitFunc(f)
	}
	body.WriteString(g.b.String())
	g.b = saved

	g.wf("// Code generated by vegoc from the Vego JSON form. DO NOT EDIT.\n\n")
	g.wf("import * as vg from \"./vg.ts\";\n\n")
	for _, name := range g.litOrder {
		g.wf("const %s: vg.Str = vg.lit(\"%s\");\n", name, jsEscape(g.litsKey(name)))
	}
	if len(g.litOrder) > 0 {
		g.wf("\n")
	}
	g.b.WriteString(body.String())
	return g.b.String()
}

// litsKey finds the literal bytes behind a hoisted constant name.
func (g *gen) litsKey(name string) string {
	for k, v := range g.lits {
		if v == name {
			return k
		}
	}
	fatal("unknown literal constant", name)
	return ""
}

func (g *gen) constDecl(d *compiler.ValueDecl) {
	t := d.Inferred
	if t == nil {
		t = d.Type
	}
	if d.ConstVal != nil {
		// An untyped constant defaults to int, but a value without an exact number form cannot be one.
		// The declaration then holds a bigint, which is the only exact representation the host can use.
		if !isBig(t) && !exactNumber(d.ConstVal) {
			g.wf("export const %s: bigint = %sn;\n", ident(d.Name), d.ConstVal.String())
			return
		}
		g.wf("export const %s: %s = %s;\n", ident(d.Name), g.typ(t), g.constLit(d.ConstVal, t))
		return
	}
	g.wf("export const %s: %s = %s;\n", ident(d.Name), g.typ(t), g.expr(d.Value))
}

// exactNumber reports whether a JavaScript number holds v exactly.
// Every integer up to 2^53 qualifies, and so do sparse larger ones such as powers of two.
// A constant past 2^53 can therefore serve as a sentinel that is only compared, while the runtime check on arithmetic results stays strict.
func exactNumber(v *big.Int) bool {
	f := new(big.Float).SetPrec(53).SetInt(v)
	back, _ := f.Int(nil)
	return back.Cmp(v) == 0
}

func isBig(t *compiler.Type) bool {
	return t.K == compiler.KI64 || t.K == compiler.KU64
}

// constLit renders a folded integer constant at the given type.
func (g *gen) constLit(v *big.Int, t *compiler.Type) string {
	if t != nil && isBig(t) {
		return v.String() + "n"
	}
	if !exactNumber(v) {
		fatal("integer constant", v.String(), "has no exact JavaScript number form")
	}
	if v.Sign() < 0 {
		return "(" + v.String() + ")"
	}
	return v.String()
}

func (g *gen) typ(t *compiler.Type) string {
	switch t.K {
	case compiler.KBool:
		return "boolean"
	case compiler.KU8, compiler.KU16, compiler.KU32, compiler.KI32, compiler.KInt:
		return "number"
	case compiler.KI64, compiler.KU64:
		return "bigint"
	case compiler.KStr:
		return "vg.Str"
	case compiler.KSlice:
		return "vg.Slice<" + g.typ(t.Elem) + ">"
	case compiler.KArray:
		return g.arrayType(t)
	case compiler.KStruct, compiler.KPtr:
		return ident(t.Name)
	}
	fatal("cannot map type", t)
	return ""
}

// arrayType names the buffer type of a fixed-size array: a typed array for scalars, a plain array otherwise.
func (g *gen) arrayType(t *compiler.Type) string {
	if name := typedArray(t.Elem); name != "" {
		return name
	}
	return g.typ(t.Elem) + "[]"
}

func typedArray(elem *compiler.Type) string {
	switch elem.K {
	case compiler.KU8:
		return "Uint8Array"
	case compiler.KU16:
		return "Uint16Array"
	case compiler.KU32:
		return "Uint32Array"
	case compiler.KI32:
		return "Int32Array"
	case compiler.KInt:
		return "Float64Array"
	case compiler.KI64:
		return "BigInt64Array"
	case compiler.KU64:
		return "BigUint64Array"
	}
	return ""
}

// elem names the runtime descriptor of an element type, for make, append and copy.
func (g *gen) elem(t *compiler.Type) string {
	switch t.K {
	case compiler.KBool:
		return "vg.BOOL"
	case compiler.KU8:
		return "vg.U8"
	case compiler.KU16:
		return "vg.U16"
	case compiler.KU32:
		return "vg.U32"
	case compiler.KI32:
		return "vg.I32"
	case compiler.KInt:
		return "vg.INT"
	case compiler.KI64:
		return "vg.I64"
	case compiler.KU64:
		return "vg.U64"
	case compiler.KStr:
		return "vg.STR"
	case compiler.KSlice:
		return "vg.SLICE"
	case compiler.KStruct:
		return ident(t.Name) + ".elem"
	case compiler.KArray:
		return fmt.Sprintf("vg.arrayElem(() => %s, (a) => %s, %d)", g.zero(t), g.cloneOf(t, "a"), g.width(t))
	}
	fatal("no element descriptor for", t)
	return ""
}

// width estimates the bytes of one value, for the allocation counters of the bench host.
func (g *gen) width(t *compiler.Type) int {
	switch t.K {
	case compiler.KBool, compiler.KU8:
		return 1
	case compiler.KU16:
		return 2
	case compiler.KU32, compiler.KI32:
		return 4
	case compiler.KInt, compiler.KI64, compiler.KU64:
		return 8
	case compiler.KStr:
		return 16
	case compiler.KSlice:
		return 32
	case compiler.KArray:
		n, _ := g.p.ArrayLen(t)
		return int(n) * g.width(t.Elem)
	case compiler.KStruct:
		total := 0
		for _, f := range g.p.StructMap[t.Name].Fields {
			total += g.width(f.Type)
		}
		return total
	}
	return 8
}

func (g *gen) zero(t *compiler.Type) string {
	switch t.K {
	case compiler.KBool:
		return "false"
	case compiler.KU8, compiler.KU16, compiler.KU32, compiler.KI32, compiler.KInt:
		return "0"
	case compiler.KI64, compiler.KU64:
		return "0n"
	case compiler.KStr:
		return "vg.EMPTY"
	case compiler.KSlice:
		return "vg.NIL"
	case compiler.KStruct:
		return "new " + ident(t.Name) + "()"
	case compiler.KArray:
		n := g.arrayLen(t)
		if name := typedArray(t.Elem); name != "" {
			return fmt.Sprintf("new %s(%d)", name, n)
		}
		return fmt.Sprintf("vg.makeArr(%d, () => %s)", n, g.zero(t.Elem))
	}
	fatal("no zero value for", t)
	return ""
}

func (g *gen) arrayLen(t *compiler.Type) int64 {
	n, ok := g.p.ArrayLen(t)
	if !ok {
		fatal("unresolved array length")
	}
	return n
}

// needsClone reports whether a value of type t has identity in JavaScript while Go copies it.
func needsClone(t *compiler.Type) bool {
	return t != nil && (t.K == compiler.KStruct || t.K == compiler.KArray)
}

// cloneOf renders a deep copy of the value expression code, of type t.
func (g *gen) cloneOf(t *compiler.Type, code string) string {
	switch t.K {
	case compiler.KStruct:
		return code + ".clone()"
	case compiler.KArray:
		if !needsClone(t.Elem) {
			return code + ".slice()"
		}
		return code + ".map((v) => " + g.cloneOf(t.Elem, "v") + ")"
	}
	return code
}

// eqOf renders a comparison of two values of type t.
func (g *gen) eqOf(t *compiler.Type, a, b string) string {
	switch t.K {
	case compiler.KStr:
		return fmt.Sprintf("vg.streq(%s, %s)", a, b)
	case compiler.KStruct:
		return fmt.Sprintf("%s.eq(%s, %s)", ident(t.Name), a, b)
	case compiler.KArray:
		return fmt.Sprintf("vg.arrEq(%s, %s, (x, y) => %s)", a, b, g.eqOf(t.Elem, "x", "y"))
	case compiler.KSlice:
		fatal("comparison of slice values")
	}
	return fmt.Sprintf("%s === %s", a, b)
}

func comparable(g *gen, t *compiler.Type) bool {
	switch t.K {
	case compiler.KSlice:
		return false
	case compiler.KArray:
		return comparable(g, t.Elem)
	case compiler.KStruct:
		for _, f := range g.p.StructMap[t.Name].Fields {
			if !comparable(g, f.Type) {
				return false
			}
		}
	}
	return true
}

func (g *gen) structDecl(s *compiler.StructDecl) {
	name := ident(s.Name)
	g.wf("export class %s {\n", name)
	for _, f := range s.Fields {
		g.wf("    %s: %s;\n", field(f.Name), g.typ(f.Type))
	}
	g.wf("\n")
	var params, cloneArgs []string
	for _, f := range s.Fields {
		params = append(params, fmt.Sprintf("%s: %s = %s", ident(f.Name), g.typ(f.Type), g.zero(f.Type)))
		cloneArgs = append(cloneArgs, g.cloneOf(f.Type, "this."+field(f.Name)))
	}
	g.wf("    constructor(%s) {\n", strings.Join(params, ", "))
	for _, f := range s.Fields {
		g.wf("        this.%s = %s;\n", field(f.Name), ident(f.Name))
	}
	g.wf("    }\n\n")
	g.wf("    clone(): %s {\n", name)
	g.wf("        return new %s(%s);\n", name, strings.Join(cloneArgs, ", "))
	g.wf("    }\n")
	if comparable(g, &compiler.Type{K: compiler.KStruct, Name: s.Name}) {
		var terms []string
		for _, f := range s.Fields {
			terms = append(terms, g.eqOf(f.Type, "a."+field(f.Name), "b."+field(f.Name)))
		}
		if len(terms) == 0 {
			terms = append(terms, "true")
		}
		g.wf("\n    static eq(a: %s, b: %s): boolean {\n", name, name)
		g.wf("        return %s;\n", strings.Join(terms, " && "))
		g.wf("    }\n")
	}
	g.wf("\n    static readonly elem: vg.Elem<%s> = vg.structElem(() => new %s(), %d);\n",
		name, name, g.width(&compiler.Type{K: compiler.KStruct, Name: s.Name}))
	g.wf("}\n\n")
}

func (g *gen) emitFunc(f *compiler.FuncDecl) {
	g.fn = f
	g.resetNames(f)
	g.temps = nil
	g.loops = nil
	g.switchDepth = 0
	var params []string
	for _, pa := range f.Params {
		params = append(params, ident(pa.Name)+": "+g.typ(pa.Type))
	}
	ret := "void"
	switch len(f.Results) {
	case 1:
		ret = g.typ(f.Results[0])
	case 2:
		ret = "[" + g.typ(f.Results[0]) + ", " + g.typ(f.Results[1]) + "]"
	}
	saved := g.b
	g.b = strings.Builder{}
	g.body(f.Body, 1)
	body := g.b.String()
	g.b = saved
	g.wf("export function %s(%s): %s {\n", ident(f.Name), strings.Join(params, ", "), ret)
	for _, t := range g.temps {
		g.wf("    %s\n", t)
	}
	g.b.WriteString(body)
	g.wf("}\n\n")
	g.fn = nil
}

func (g *gen) indent(depth int) {
	for i := 0; i < depth; i++ {
		g.b.WriteString("    ")
	}
}

func (g *gen) body(body []*compiler.Stmt, depth int) {
	for _, s := range body {
		g.stmt(s, depth)
	}
}

func (g *gen) declKw(name string) string {
	if info := g.fn.Info[name]; info != nil && info.Mutated {
		return "let"
	}
	return "const"
}

func (g *gen) declLine(depth int, name string, t *compiler.Type, value string) {
	g.indent(depth)
	g.wf("%s %s: %s = %s;\n", g.declKw(name), ident(name), g.typ(t), value)
}

// stored renders a value that a variable, field, element or append will hold.
// A struct or array read from a place is cloned, because the store must not alias the place.
func (g *gen) stored(e *compiler.Expr) string {
	code := g.expr(e)
	if needsClone(e.Typ) && isPlace(e) {
		return g.cloneOf(e.Typ, code)
	}
	return code
}

// returned renders a return value.
// A local that is not a parameter owns its value, so it needs no copy; every other place does.
func (g *gen) returned(e *compiler.Expr) string {
	code := g.expr(e)
	if !needsClone(e.Typ) || !isPlace(e) {
		return code
	}
	if e.K == "ident" && !g.isParam(e.Name) {
		return code
	}
	return g.cloneOf(e.Typ, code)
}

func (g *gen) isParam(name string) bool {
	for _, pa := range g.fn.Params {
		if pa.Name == name {
			return true
		}
	}
	return false
}

func isPlace(e *compiler.Expr) bool {
	switch e.K {
	case "ident":
		return e.Name != "nil"
	case "field", "index":
		return true
	}
	return false
}

func (g *gen) stmt(s *compiler.Stmt, depth int) {
	switch s.K {
	case "var_decl":
		val := g.zero(s.TypeRef)
		if s.Value != nil {
			val = g.stored(s.Value)
		}
		g.declLine(depth, s.Name, s.TypeRef, val)
	case "define":
		if len(s.Names) == 1 {
			g.declLine(depth, s.Names[0], s.DeclaredTypes[0], g.stored(s.Value))
			return
		}
		tmp := g.newTmp()
		g.indent(depth)
		g.wf("const %s = %s;\n", tmp, g.expr(s.Value))
		for i, n := range s.Names {
			if n == "_" {
				continue
			}
			g.declLine(depth, n, s.DeclaredTypes[i], fmt.Sprintf("%s[%d]", tmp, i))
		}
	case "assign":
		if len(s.Lhs) == 2 {
			tmp := g.newTmp()
			g.indent(depth)
			g.wf("{\n")
			g.indent(depth + 1)
			g.wf("const %s = %s;\n", tmp, g.expr(s.Value))
			for i, l := range s.Lhs {
				if l.K == "ident" && l.Name == "_" {
					continue
				}
				g.indent(depth + 1)
				g.wf("%s;\n", g.store(l, fmt.Sprintf("%s[%d]", tmp, i), false))
			}
			g.indent(depth)
			g.wf("}\n")
			return
		}
		g.indent(depth)
		l := s.Lhs[0]
		if l.K == "ident" && l.Name == "_" {
			g.wf("%s;\n", g.expr(s.Value))
			return
		}
		g.wf("%s;\n", g.store(l, g.stored(s.Value), compiler.Impure(s.Value)))
	case "op_assign":
		g.indent(depth)
		g.wf("%s;\n", g.opAssign(s))
	case "if":
		g.indent(depth)
		g.wf("if (%s) {\n", g.expr(s.Cond))
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
		g.emitSwitch(s, depth)
	case "break":
		g.indent(depth)
		if g.switchDepth > 0 && len(g.loops) > 0 {
			loop := g.loops[len(g.loops)-1]
			loop.labeled = true
			g.wf("break %s;\n", loop.label)
			return
		}
		g.wf("break;\n")
	case "continue":
		g.indent(depth)
		g.wf("continue;\n")
	case "return":
		g.indent(depth)
		switch len(s.Values) {
		case 0:
			g.wf("return;\n")
		case 1:
			g.wf("return %s;\n", g.returned(s.Values[0]))
		default:
			g.wf("return [%s, %s];\n", g.returned(s.Values[0]), g.returned(s.Values[1]))
		}
	case "expr_stmt":
		g.indent(depth)
		g.wf("%s;\n", g.expr(s.Value))
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

// loopBody emits a loop body under a fresh loop record and returns the body text and the label it needs, if any.
func (g *gen) loopBody(body []*compiler.Stmt, depth int) (string, *loopInfo) {
	info := &loopInfo{label: g.newTmp()}
	g.loops = append(g.loops, info)
	savedDepth := g.switchDepth
	g.switchDepth = 0
	saved := g.b
	g.b = strings.Builder{}
	g.body(body, depth)
	text := g.b.String()
	g.b = saved
	g.switchDepth = savedDepth
	g.loops = g.loops[:len(g.loops)-1]
	return text, info
}

func (g *gen) labelPrefix(info *loopInfo) string {
	if info.labeled {
		return info.label + ": "
	}
	return ""
}

func (g *gen) emitFor(s *compiler.Stmt, depth int) {
	cond := "true"
	if s.Cond != nil {
		cond = g.expr(s.Cond)
	}
	if s.Init == nil && s.Post == nil {
		text, info := g.loopBody(s.Body, depth+1)
		g.indent(depth)
		g.wf("%swhile (%s) {\n", g.labelPrefix(info), cond)
		g.b.WriteString(text)
		g.indent(depth)
		g.wf("}\n")
		return
	}
	init := ""
	if s.Init != nil {
		init = g.inlineInit(s.Init)
	}
	post := ""
	if s.Post != nil {
		post = g.inlineStmt(s.Post)
	}
	text, info := g.loopBody(s.Body, depth+1)
	g.indent(depth)
	g.wf("%sfor (%s; %s; %s) {\n", g.labelPrefix(info), init, cond, post)
	g.b.WriteString(text)
	g.indent(depth)
	g.wf("}\n")
}

// inlineInit renders a loop init statement inside the for header.
// The variable is always let, because the post statement writes it.
func (g *gen) inlineInit(s *compiler.Stmt) string {
	switch s.K {
	case "define":
		if len(s.Names) != 1 {
			fatal("two-value define in loop init")
		}
		return fmt.Sprintf("let %s: %s = %s", ident(s.Names[0]), g.typ(s.DeclaredTypes[0]), g.stored(s.Value))
	case "var_decl":
		val := g.zero(s.TypeRef)
		if s.Value != nil {
			val = g.stored(s.Value)
		}
		return fmt.Sprintf("let %s: %s = %s", ident(s.Name), g.typ(s.TypeRef), val)
	case "assign", "op_assign", "expr_stmt":
		return g.inlineStmt(s)
	}
	fatal("unsupported loop init statement", s.K)
	return ""
}

// inlineStmt renders a simple statement as one expression, for loop headers.
func (g *gen) inlineStmt(s *compiler.Stmt) string {
	switch s.K {
	case "assign":
		if len(s.Lhs) != 1 {
			fatal("two-value assign in loop post")
		}
		return g.store(s.Lhs[0], g.stored(s.Value), compiler.Impure(s.Value))
	case "op_assign":
		return g.opAssign(s)
	case "expr_stmt":
		return g.expr(s.Value)
	}
	fatal("unsupported loop post statement", s.K)
	return ""
}

// emitRange lowers a range statement to an index loop.
// The operand evaluates once, and a hidden counter drives the loop.
// A write to the user variables in the body therefore cannot change the iteration.
func (g *gen) emitRange(s *compiler.Stmt, depth int) {
	over := g.newTmp()
	counter := g.newTmp()
	g.indent(depth)
	g.wf("{\n")
	g.indent(depth + 1)
	// Go ranges over a copy of an array value, so a write to the array inside the body cannot reach a later element.
	operand := g.expr(s.Value)
	if s.Value.Typ.K == compiler.KArray && s.ValName != "" && s.ValName != "_" {
		operand = g.stored(s.Value)
	}
	g.wf("const %s = %s;\n", over, operand)
	limit := over
	switch s.Value.Typ.K {
	case compiler.KSlice:
		limit = over + ".len"
	case compiler.KArray:
		limit = strconv.FormatInt(g.arrayLen(s.Value.Typ), 10)
	}
	text, info := g.loopBody(s.Body, depth+2)
	g.indent(depth + 1)
	g.wf("%sfor (let %s = 0; %s < %s; %s++) {\n", g.labelPrefix(info), counter, counter, limit, counter)
	if s.IdxName != "" && s.IdxName != "_" {
		g.declLine(depth+2, s.IdxName, compiler.TInt, counter)
	}
	if s.ValName != "" && s.ValName != "_" {
		read := &compiler.Expr{K: "index", Typ: s.Value.Typ.Elem,
			X:     &compiler.Expr{K: "ident", Name: over, Typ: s.Value.Typ},
			Index: &compiler.Expr{K: "ident", Name: counter, Typ: compiler.TInt}}
		g.declLine(depth+2, s.ValName, s.Value.Typ.Elem, g.stored(read))
	}
	g.b.WriteString(text)
	g.indent(depth + 1)
	g.wf("}\n")
	g.indent(depth)
	g.wf("}\n")
}

func (g *gen) emitSwitch(s *compiler.Stmt, depth int) {
	if s.Tag.Typ != nil && s.Tag.Typ.K == compiler.KStr {
		fatal("switch on a string tag")
	}
	g.indent(depth)
	g.wf("switch (%s) {\n", g.expr(s.Tag))
	g.switchDepth++
	for _, cs := range s.Cases {
		for _, v := range cs.Values {
			g.indent(depth + 1)
			g.wf("case %s:\n", g.expr(v))
		}
		g.indent(depth + 1)
		g.wf("{\n")
		g.body(cs.Body, depth+2)
		g.indent(depth + 2)
		g.wf("break;\n")
		g.indent(depth + 1)
		g.wf("}\n")
	}
	if s.HasDef {
		g.indent(depth + 1)
		g.wf("default:\n")
		g.indent(depth + 1)
		g.wf("{\n")
		g.body(s.Default, depth+2)
		g.indent(depth + 2)
		g.wf("break;\n")
		g.indent(depth + 1)
		g.wf("}\n")
	}
	g.switchDepth--
	g.indent(depth)
	g.wf("}\n")
}

func (g *gen) newTmp() string {
	if g.used == nil {
		g.resetNames(g.fn)
	}
	for {
		g.tmp++
		name := fmt.Sprintf("_t%d", g.tmp)
		if !g.used[name] {
			g.used[name] = true
			return name
		}
	}
}

// hoisted declares a temporary at the top of the function and returns its name.
// Expression-level temporaries live there, so a comma expression can assign and read them in place.
func (g *gen) hoisted(t *compiler.Type) string {
	name := g.newTmp()
	g.temps = append(g.temps, fmt.Sprintf("let %s!: %s;", name, g.typ(t)))
	return name
}

func (g *gen) resetNames(f *compiler.FuncDecl) {
	g.tmp = 0
	g.used = map[string]bool{}
	reserve := func(name string) { g.used[ident(name)] = true }
	for _, d := range g.p.Consts {
		reserve(d.Name)
	}
	for _, d := range g.p.Vars {
		reserve(d.Name)
	}
	for _, d := range g.p.Types {
		reserve(d.Name)
	}
	for _, d := range g.p.Funcs {
		reserve(d.Name)
	}
	if f != nil {
		for name := range f.Info {
			reserve(name)
		}
	}
}

// store renders an assignment of value code into the place l.
// When the index or the value has side effects, the base and the index are pinned first, so they evaluate once and before the value, as in Go.
func (g *gen) store(l *compiler.Expr, value string, impureValue bool) string {
	if l.K != "index" {
		return g.expr(l) + " = " + value
	}
	base := l.X
	switch base.Typ.K {
	case compiler.KSlice:
		if !impureValue && !g.needsPin(base, l.Index) {
			b := g.expr(base)
			return fmt.Sprintf("%s.buf[%s.off + vg.ix(%s, %s.len)] = %s", b, b, g.idx(l.Index), b, value)
		}
		bt := g.hoisted(base.Typ)
		it := g.hoisted(compiler.TInt)
		return fmt.Sprintf("(%s = %s, %s = vg.ix(%s, %s.len), %s.buf[%s.off + %s] = %s)",
			bt, g.expr(base), it, g.idx(l.Index), bt, bt, bt, it, value)
	case compiler.KArray:
		n := g.arrayLen(base.Typ)
		if !impureValue && !compiler.Impure(l.Index) && !compiler.Impure(base) {
			return fmt.Sprintf("%s[%s] = %s", g.expr(base), g.arrIdx(l.Index, n), value)
		}
		bt := g.hoisted(base.Typ)
		it := g.hoisted(compiler.TInt)
		return fmt.Sprintf("(%s = %s, %s = %s, %s[%s] = %s)",
			bt, g.expr(base), it, g.arrIdx(l.Index, n), bt, it, value)
	}
	fatal("store into", base.Typ)
	return ""
}

// arrIdx renders a checked array index; a constant in range needs no check.
func (g *gen) arrIdx(e *compiler.Expr, n int64) string {
	if e.IsConst {
		if v := g.constOf(e); v != nil && v.Sign() >= 0 && v.Cmp(big.NewInt(n)) < 0 {
			return v.String()
		}
	}
	return fmt.Sprintf("vg.ix(%s, %d)", g.idx(e), n)
}

// constOf folds a constant expression, or returns nil when it is not an integer constant.
func (g *gen) constOf(e *compiler.Expr) *big.Int {
	switch e.K {
	case "int", "char":
		v, ok := new(big.Int).SetString(e.Value, 10)
		if !ok {
			return nil
		}
		return v
	case "ident":
		if d, ok := g.p.ConstMap[e.Name]; ok && d.ConstVal != nil {
			return new(big.Int).Set(d.ConstVal)
		}
	}
	return nil
}

func (g *gen) opAssign(s *compiler.Stmt) string {
	l := s.Lhs[0]
	if l.K != "index" || (!g.needsPin(l.X, l.Index) && !compiler.Impure(s.Value) && l.X.Typ.K == compiler.KSlice) ||
		(l.X.Typ.K == compiler.KArray && !compiler.Impure(l.Index) && !compiler.Impure(l.X) && !compiler.Impure(s.Value)) {
		place := g.expr(l)
		return place + " = " + g.arith(s.Op[:len(s.Op)-1], l.Typ, place, s.Value)
	}
	// The place evaluates once and before the value, so its parts are pinned.
	base := l.X
	bt := g.hoisted(base.Typ)
	it := g.hoisted(compiler.TInt)
	var place, pin string
	switch base.Typ.K {
	case compiler.KSlice:
		place = fmt.Sprintf("%s.buf[%s.off + %s]", bt, bt, it)
		pin = fmt.Sprintf("%s = %s, %s = vg.ix(%s, %s.len)", bt, g.expr(base), it, g.idx(l.Index), bt)
	case compiler.KArray:
		place = fmt.Sprintf("%s[%s]", bt, it)
		pin = fmt.Sprintf("%s = %s, %s = %s", bt, g.expr(base), it, g.arrIdx(l.Index, g.arrayLen(base.Typ)))
	default:
		fatal("compound assignment into", base.Typ)
	}
	return fmt.Sprintf("(%s, %s = %s)", pin, place, g.arith(s.Op[:len(s.Op)-1], l.Typ, place, s.Value))
}

// idx renders an index or bound expression as a number.
func (g *gen) idx(e *compiler.Expr) string {
	if isBig(e.Typ) {
		return "vg.intOf(" + g.expr(e) + ")"
	}
	return g.expr(e)
}

func (g *gen) expr(e *compiler.Expr) string {
	switch e.K {
	case "int", "char":
		v, ok := new(big.Int).SetString(e.Value, 10)
		if !ok {
			fatal("bad integer literal", e.Value)
		}
		return g.constLit(v, e.Typ)
	case "str":
		return g.strLit(e.Value)
	case "bool":
		if e.BoolVal {
			return "true"
		}
		return "false"
	case "ident":
		if e.Name == "nil" {
			return "vg.NIL"
		}
		if d, ok := g.p.ConstMap[e.Name]; ok && (g.fn == nil || g.fn.Info[e.Name] == nil) {
			return g.constRef(d, e.Typ)
		}
		return ident(e.Name)
	case "field":
		return g.expr(e.X) + "." + field(e.Name)
	case "index":
		return g.index(e)
	case "slice_expr":
		x := g.expr(e.X)
		switch e.X.Typ.K {
		case compiler.KSlice:
			switch {
			case e.Lo == nil && e.Hi == nil:
				return x
			case e.Lo == nil:
				return fmt.Sprintf("vg.head(%s, %s)", x, g.idx(e.Hi))
			case e.Hi == nil:
				return fmt.Sprintf("vg.tail(%s, %s)", x, g.idx(e.Lo))
			default:
				return fmt.Sprintf("vg.sub(%s, %s, %s)", x, g.idx(e.Lo), g.idx(e.Hi))
			}
		case compiler.KStr:
			switch {
			case e.Lo == nil && e.Hi == nil:
				return x
			case e.Lo == nil:
				return fmt.Sprintf("vg.strHead(%s, %s)", x, g.idx(e.Hi))
			case e.Hi == nil:
				return fmt.Sprintf("vg.strTail(%s, %s)", x, g.idx(e.Lo))
			default:
				return fmt.Sprintf("vg.strSub(%s, %s, %s)", x, g.idx(e.Lo), g.idx(e.Hi))
			}
		case compiler.KArray:
			lo := "0"
			if e.Lo != nil {
				lo = g.idx(e.Lo)
			}
			hi := strconv.FormatInt(g.arrayLen(e.X.Typ), 10)
			if e.Hi != nil {
				hi = g.idx(e.Hi)
			}
			return fmt.Sprintf("vg.arrSlice(%s, %s, %s)", x, lo, hi)
		}
		fatal("slice of", e.X.Typ)
	case "call":
		var args []string
		for _, a := range e.Args {
			args = append(args, g.expr(a))
		}
		return ident(e.Name) + "(" + strings.Join(args, ", ") + ")"
	case "builtin":
		return g.builtin(e)
	case "conv":
		return g.conv(e)
	case "unary":
		return g.unary(e)
	case "binary":
		return g.binary(e)
	case "composite":
		return g.composite(e)
	}
	fatal("unknown expression kind", e.K)
	return ""
}

// constRef renders a use of a named constant at the type its context gave it.
// A typed constant, and an untyped one used at its default number type, keep their name.
// An untyped constant used as a bigint, or one past the exact number range, becomes its folded value.
func (g *gen) constRef(d *compiler.ValueDecl, t *compiler.Type) string {
	if d.ConstVal == nil || d.Type != nil {
		return ident(d.Name)
	}
	if t == nil {
		t = d.Inferred
	}
	if isBig(t) {
		return g.constLit(d.ConstVal, t)
	}
	if !exactNumber(d.ConstVal) {
		fatal("constant", d.Name, "used as an int has no exact JavaScript number form:", d.ConstVal.String())
	}
	return ident(d.Name)
}

func (g *gen) strLit(s string) string {
	if s == "" {
		return "vg.EMPTY"
	}
	if name, ok := g.lits[s]; ok {
		return name
	}
	name := fmt.Sprintf("_s%d", len(g.litOrder)+1)
	g.lits[s] = name
	g.litOrder = append(g.litOrder, name)
	return name
}

// index renders an element read.
// The base appears more than once in the rendered form, so anything but a plain identifier is pinned into a temporary.
// The temporary also makes the base evaluate once and before the index, as in Go, when the index has side effects.
func (g *gen) index(e *compiler.Expr) string {
	switch e.X.Typ.K {
	case compiler.KSlice:
		if g.needsPin(e.X, e.Index) {
			t := g.hoisted(e.X.Typ)
			return fmt.Sprintf("(%s = %s, %s.buf[%s.off + vg.ix(%s, %s.len)])", t, g.expr(e.X), t, t, g.idx(e.Index), t)
		}
		x := g.expr(e.X)
		return fmt.Sprintf("%s.buf[%s.off + vg.ix(%s, %s.len)]", x, x, g.idx(e.Index), x)
	case compiler.KStr:
		if g.needsPin(e.X, e.Index) {
			t := g.hoisted(e.X.Typ)
			return fmt.Sprintf("(%s = %s, %s[vg.ix(%s, %s.length)])", t, g.expr(e.X), t, g.idx(e.Index), t)
		}
		x := g.expr(e.X)
		return fmt.Sprintf("%s[vg.ix(%s, %s.length)]", x, g.idx(e.Index), x)
	case compiler.KArray:
		n := g.arrayLen(e.X.Typ)
		if compiler.Impure(e.Index) || compiler.Impure(e.X) {
			t := g.hoisted(e.X.Typ)
			return fmt.Sprintf("(%s = %s, %s[%s])", t, g.expr(e.X), t, g.arrIdx(e.Index, n))
		}
		return fmt.Sprintf("%s[%s]", g.expr(e.X), g.arrIdx(e.Index, n))
	}
	fatal("index of", e.X.Typ)
	return ""
}

// needsPin reports whether an indexed base must go through a temporary.
// A plain identifier reads cheaply as often as needed; everything else is a chain of loads or a call.
func (g *gen) needsPin(base, index *compiler.Expr) bool {
	return base.K != "ident" || compiler.Impure(index)
}

func (g *gen) builtin(e *compiler.Expr) string {
	switch e.Name {
	case "len":
		switch e.Args[0].Typ.K {
		case compiler.KStr:
			return g.expr(e.Args[0]) + ".length"
		case compiler.KSlice:
			return g.expr(e.Args[0]) + ".len"
		case compiler.KArray:
			return strconv.FormatInt(g.arrayLen(e.Args[0].Typ), 10)
		}
		fatal("len of", e.Args[0].Typ)
	case "cap":
		return g.expr(e.Args[0]) + ".cap"
	case "make":
		el := g.elem(e.TypeRef.Elem)
		if len(e.Args) == 2 {
			return fmt.Sprintf("vg.make(%s, %s, %s)", el, g.idx(e.Args[0]), g.idx(e.Args[1]))
		}
		return fmt.Sprintf("vg.make(%s, %s)", el, g.idx(e.Args[0]))
	case "append":
		st := e.Args[0].Typ
		el := g.elem(st.Elem)
		if e.Spread {
			if e.Args[1].Typ.K == compiler.KStr {
				return fmt.Sprintf("vg.appendStr(%s, %s)", g.expr(e.Args[0]), g.expr(e.Args[1]))
			}
			return fmt.Sprintf("vg.appendSlice(%s, %s, %s)", el, g.expr(e.Args[0]), g.expr(e.Args[1]))
		}
		if compiler.AppendNeedsPin(e) {
			// Every element evaluates before the first write, as in Go.
			var vals []string
			for _, a := range e.Args[1:] {
				vals = append(vals, g.stored(a))
			}
			return fmt.Sprintf("vg.appendMany(%s, %s, %s)", el, g.expr(e.Args[0]), strings.Join(vals, ", "))
		}
		out := g.expr(e.Args[0])
		for _, a := range e.Args[1:] {
			out = fmt.Sprintf("vg.append(%s, %s, %s)", el, out, g.stored(a))
		}
		return out
	case "copy":
		if e.Args[1].Typ.K == compiler.KStr {
			return fmt.Sprintf("vg.copyStr(%s, %s)", g.expr(e.Args[0]), g.expr(e.Args[1]))
		}
		return fmt.Sprintf("vg.copy(%s, %s, %s)", g.elem(e.Args[0].Typ.Elem), g.expr(e.Args[0]), g.expr(e.Args[1]))
	case "min", "max":
		if isBig(e.Typ) {
			return fmt.Sprintf("vg.%sBig(%s, %s)", e.Name, g.expr(e.Args[0]), g.expr(e.Args[1]))
		}
		return fmt.Sprintf("Math.%s(%s, %s)", e.Name, g.expr(e.Args[0]), g.expr(e.Args[1]))
	}
	fatal("unknown builtin", e.Name)
	return ""
}

// conv renders an integer conversion with Go semantics: widen with the sign of the source, then truncate to the target width.
func (g *gen) conv(e *compiler.Expr) string {
	to := e.TypeRef
	if to.K == compiler.KStr {
		return "vg.strFromBytes(" + g.expr(e.X) + ")"
	}
	if to.K == compiler.KSlice {
		return "vg.bytesFromStr(" + g.expr(e.X) + ")"
	}
	if e.X.IsConst {
		if v := g.foldConst(e.X); v != nil {
			return g.constLit(truncate(v, to), to)
		}
	}
	x := g.expr(e.X)
	from := e.X.Typ
	if isBig(from) {
		switch to.K {
		case compiler.KU8:
			return "Number(BigInt.asUintN(8, " + x + "))"
		case compiler.KU16:
			return "Number(BigInt.asUintN(16, " + x + "))"
		case compiler.KU32:
			return "Number(BigInt.asUintN(32, " + x + "))"
		case compiler.KI32:
			return "Number(BigInt.asIntN(32, " + x + "))"
		case compiler.KInt:
			return "vg.intOf(" + x + ")"
		case compiler.KI64:
			if from.K == compiler.KI64 {
				return x
			}
			return "BigInt.asIntN(64, " + x + ")"
		case compiler.KU64:
			if from.K == compiler.KU64 {
				return x
			}
			return "BigInt.asUintN(64, " + x + ")"
		}
		fatal("conversion to", to)
	}
	switch to.K {
	case compiler.KU8:
		if from.K == compiler.KU8 {
			return x
		}
		return "(" + x + " & 0xff)"
	case compiler.KU16:
		if from.K == compiler.KU8 || from.K == compiler.KU16 {
			return x
		}
		return "(" + x + " & 0xffff)"
	case compiler.KU32:
		if from.K == compiler.KU8 || from.K == compiler.KU16 || from.K == compiler.KU32 {
			return x
		}
		return "(" + x + " >>> 0)"
	case compiler.KI32:
		if from.K == compiler.KU8 || from.K == compiler.KU16 || from.K == compiler.KI32 {
			return x
		}
		return "(" + x + " | 0)"
	case compiler.KInt:
		return x
	case compiler.KI64:
		return "BigInt(" + x + ")"
	case compiler.KU64:
		if from.K == compiler.KInt || from.K == compiler.KI32 {
			return "BigInt.asUintN(64, BigInt(" + x + "))"
		}
		return "BigInt(" + x + ")"
	}
	fatal("conversion to", to)
	return ""
}

// foldConst evaluates a constant expression, or returns nil when the printer cannot fold it.
// The checker folded every constant declaration; this covers the literal forms that conversions apply to.
func (g *gen) foldConst(e *compiler.Expr) *big.Int {
	switch e.K {
	case "int", "char", "ident":
		return g.constOf(e)
	case "unary":
		v := g.foldConst(e.X)
		if v == nil {
			return nil
		}
		switch e.Op {
		case "-":
			return v.Neg(v)
		case "^":
			return v.Not(v)
		}
	case "conv":
		v := g.foldConst(e.X)
		if v == nil {
			return nil
		}
		return truncate(v, e.TypeRef)
	case "binary":
		x, y := g.foldConst(e.X), g.foldConst(e.Y)
		if x == nil || y == nil {
			return nil
		}
		switch e.Op {
		case "+":
			return x.Add(x, y)
		case "-":
			return x.Sub(x, y)
		case "*":
			return x.Mul(x, y)
		case "<<":
			return x.Lsh(x, uint(y.Uint64()))
		case ">>":
			return x.Rsh(x, uint(y.Uint64()))
		case "|":
			return x.Or(x, y)
		case "&":
			return x.And(x, y)
		case "^":
			return x.Xor(x, y)
		}
	}
	return nil
}

// truncate reduces a value to the width and sign of an integer type, as a Go conversion does.
func truncate(v *big.Int, t *compiler.Type) *big.Int {
	if !t.IsInteger() {
		return v
	}
	width := uint(t.Width())
	mod := new(big.Int).Lsh(big.NewInt(1), width)
	out := new(big.Int).Mod(v, mod)
	if t.Signed() {
		half := new(big.Int).Rsh(mod, 1)
		if out.Cmp(half) >= 0 {
			out.Sub(out, mod)
		}
	}
	return out
}

func (g *gen) unary(e *compiler.Expr) string {
	x := g.expr(e.X)
	switch e.Op {
	case "!":
		return "(!" + x + ")"
	case "&":
		return x
	case "-":
		switch e.Typ.K {
		case compiler.KU8:
			return "((-" + x + ") & 0xff)"
		case compiler.KU16:
			return "((-" + x + ") & 0xffff)"
		case compiler.KU32:
			return "((-" + x + ") >>> 0)"
		case compiler.KI32:
			return "((-" + x + ") | 0)"
		case compiler.KInt:
			return "(0 - " + x + ")"
		case compiler.KI64:
			return "BigInt.asIntN(64, -" + x + ")"
		case compiler.KU64:
			return "BigInt.asUintN(64, -" + x + ")"
		}
	case "^":
		switch e.Typ.K {
		case compiler.KU8:
			return "((~" + x + ") & 0xff)"
		case compiler.KU16:
			return "((~" + x + ") & 0xffff)"
		case compiler.KU32:
			return "((~" + x + ") >>> 0)"
		case compiler.KI32:
			return "(~" + x + ")"
		case compiler.KInt:
			return "vg.not64(" + x + ")"
		case compiler.KI64:
			return "(~" + x + ")"
		case compiler.KU64:
			return "BigInt.asUintN(64, ~" + x + ")"
		}
	}
	fatal("unknown unary", e.Op, "on", e.Typ)
	return ""
}

func isNil(e *compiler.Expr) bool {
	return e.K == "ident" && e.Name == "nil"
}

func (g *gen) binary(e *compiler.Expr) string {
	if e.Op == "==" || e.Op == "!=" {
		if isNil(e.Y) && e.X.Typ.K == compiler.KSlice {
			return "(" + g.expr(e.X) + ".buf " + e.Op + "= null)"
		}
		if isNil(e.X) && e.Y.Typ.K == compiler.KSlice {
			return "(" + g.expr(e.Y) + ".buf " + e.Op + "= null)"
		}
	}
	switch e.X.Typ.K {
	case compiler.KStr:
		x, y := g.expr(e.X), g.expr(e.Y)
		switch e.Op {
		case "==":
			return fmt.Sprintf("vg.streq(%s, %s)", x, y)
		case "!=":
			return fmt.Sprintf("(!vg.streq(%s, %s))", x, y)
		case "<", "<=", ">", ">=":
			return fmt.Sprintf("(vg.strcmp3(%s, %s) %s 0)", x, y, e.Op)
		}
	case compiler.KStruct, compiler.KArray:
		x, y := g.expr(e.X), g.expr(e.Y)
		switch e.Op {
		case "==":
			return g.eqOf(e.X.Typ, x, y)
		case "!=":
			return "(!" + g.eqOf(e.X.Typ, x, y) + ")"
		}
	case compiler.KBool:
		x, y := g.expr(e.X), g.expr(e.Y)
		switch e.Op {
		case "&&", "||":
			return fmt.Sprintf("(%s %s %s)", x, e.Op, y)
		case "==":
			return fmt.Sprintf("(%s === %s)", x, y)
		case "!=":
			return fmt.Sprintf("(%s !== %s)", x, y)
		}
	}
	switch e.Op {
	case "==":
		return fmt.Sprintf("(%s === %s)", g.expr(e.X), g.expr(e.Y))
	case "!=":
		return fmt.Sprintf("(%s !== %s)", g.expr(e.X), g.expr(e.Y))
	case "<", "<=", ">", ">=":
		return fmt.Sprintf("(%s %s %s)", g.expr(e.X), e.Op, g.expr(e.Y))
	}
	return g.arith(e.Op, e.Typ, g.expr(e.X), e.Y)
}

// arith renders an integer operator at type t.
// The left operand is already rendered; the right one is rendered here, so a shift count can take the form its width needs.
func (g *gen) arith(op string, t *compiler.Type, x string, yExpr *compiler.Expr) string {
	y := g.expr(yExpr)
	if isBig(t) {
		wrap := "BigInt.asIntN(64, %s)"
		if t.K == compiler.KU64 {
			wrap = "BigInt.asUintN(64, %s)"
		}
		switch op {
		case "+", "-", "*":
			return fmt.Sprintf(wrap, x+" "+op+" "+y)
		case "/":
			return fmt.Sprintf(wrap, fmt.Sprintf("vg.divBig(%s, %s)", x, y))
		case "%":
			return fmt.Sprintf("vg.remBig(%s, %s)", x, y)
		case "&", "|", "^":
			return fmt.Sprintf("(%s %s %s)", x, op, y)
		case "&^":
			return fmt.Sprintf(wrap, x+" & ~"+y)
		case "<<":
			return fmt.Sprintf(wrap, x+" << "+g.bigCount(yExpr))
		case ">>":
			return fmt.Sprintf("(%s >> %s)", x, g.bigCount(yExpr))
		}
		fatal("unknown operator", op)
	}
	if isBig(yExpr.Typ) && (op == "<<" || op == ">>") {
		y = "vg.intOf(" + y + ")"
	}
	switch t.K {
	case compiler.KU8, compiler.KU16:
		mask := "0xff"
		if t.K == compiler.KU16 {
			mask = "0xffff"
		}
		switch op {
		case "+", "-", "*", "<<":
			return fmt.Sprintf("((%s %s %s) & %s)", x, op, y, mask)
		case "/":
			return fmt.Sprintf("vg.div(%s, %s)", x, y)
		case "%":
			return fmt.Sprintf("vg.rem(%s, %s)", x, y)
		case "&", "|", "^":
			return fmt.Sprintf("(%s %s %s)", x, op, y)
		case "&^":
			return fmt.Sprintf("(%s & ~%s)", x, y)
		case ">>":
			return fmt.Sprintf("(%s >>> %s)", x, y)
		}
	case compiler.KU32:
		switch op {
		case "+", "-", "<<":
			return fmt.Sprintf("((%s %s %s) >>> 0)", x, op, y)
		case "*":
			return fmt.Sprintf("(Math.imul(%s, %s) >>> 0)", x, y)
		case "/":
			return fmt.Sprintf("vg.div(%s, %s)", x, y)
		case "%":
			return fmt.Sprintf("vg.rem(%s, %s)", x, y)
		case "&", "|", "^":
			return fmt.Sprintf("((%s %s %s) >>> 0)", x, op, y)
		case "&^":
			return fmt.Sprintf("((%s & ~%s) >>> 0)", x, y)
		case ">>":
			return fmt.Sprintf("(%s >>> %s)", x, y)
		}
	case compiler.KI32:
		switch op {
		case "+", "-":
			return fmt.Sprintf("((%s %s %s) | 0)", x, op, y)
		case "*":
			return fmt.Sprintf("Math.imul(%s, %s)", x, y)
		case "/":
			return fmt.Sprintf("(vg.div(%s, %s) | 0)", x, y)
		case "%":
			return fmt.Sprintf("vg.rem(%s, %s)", x, y)
		case "&", "|", "^":
			return fmt.Sprintf("(%s %s %s)", x, op, y)
		case "&^":
			return fmt.Sprintf("(%s & ~%s)", x, y)
		case "<<":
			return fmt.Sprintf("(%s << %s)", x, y)
		case ">>":
			return fmt.Sprintf("(%s >> %s)", x, y)
		}
	case compiler.KInt:
		switch op {
		case "+", "-", "*":
			return fmt.Sprintf("vg.chk(%s %s %s)", x, op, y)
		case "/":
			return fmt.Sprintf("vg.div(%s, %s)", x, y)
		case "%":
			return fmt.Sprintf("vg.rem(%s, %s)", x, y)
		case "&":
			return fmt.Sprintf("vg.and64(%s, %s)", x, y)
		case "|":
			return fmt.Sprintf("vg.or64(%s, %s)", x, y)
		case "^":
			return fmt.Sprintf("vg.xor64(%s, %s)", x, y)
		case "&^":
			return fmt.Sprintf("vg.andnot64(%s, %s)", x, y)
		case "<<":
			return fmt.Sprintf("vg.shl64(%s, %s)", x, y)
		case ">>":
			return fmt.Sprintf("vg.shr64(%s, %s)", x, y)
		}
	}
	fatal("unknown operator", op, "on", t)
	return ""
}

// bigCount renders a shift count as a bigint.
func (g *gen) bigCount(e *compiler.Expr) string {
	if e.IsConst {
		if v := g.foldConst(e); v != nil {
			return v.String() + "n"
		}
	}
	if isBig(e.Typ) {
		return g.expr(e)
	}
	return "vg.shiftCount(" + g.expr(e) + ")"
}

func (g *gen) composite(e *compiler.Expr) string {
	t := e.TypeRef
	switch t.K {
	case compiler.KStruct:
		decl := g.p.StructMap[t.Name]
		if len(e.Fields) == 0 {
			return "new " + ident(t.Name) + "()"
		}
		// Go evaluates the keyed values in source order, and the constructor takes them in declaration order.
		// When the two orders differ and a value has side effects, the values are pinned first.
		given := map[string]string{}
		var pins []string
		pin := g.compositeNeedsPin(decl, e)
		for _, f := range e.Fields {
			code := g.stored(f.Value)
			if pin {
				t := g.hoisted(f.Value.Typ)
				pins = append(pins, t+" = "+code)
				code = t
			}
			given[f.Name] = code
		}
		var args []string
		last := 0
		for i, f := range decl.Fields {
			if code, ok := given[f.Name]; ok {
				args = append(args, code)
				last = i + 1
			} else {
				args = append(args, g.zero(f.Type))
			}
		}
		out := "new " + ident(t.Name) + "(" + strings.Join(args[:last], ", ") + ")"
		if pin {
			return "(" + strings.Join(pins, ", ") + ", " + out + ")"
		}
		return out
	case compiler.KSlice:
		var parts []string
		for _, el := range e.Elems {
			parts = append(parts, g.stored(el))
		}
		return fmt.Sprintf("vg.sliceOf(%s, [%s])", g.elem(t.Elem), strings.Join(parts, ", "))
	case compiler.KArray:
		var parts []string
		for _, el := range e.Elems {
			parts = append(parts, g.stored(el))
		}
		// Go zero-fills omitted trailing elements.
		n := g.arrayLen(t)
		for int64(len(parts)) < n {
			parts = append(parts, g.zero(t.Elem))
		}
		if name := typedArray(t.Elem); name != "" {
			return fmt.Sprintf("new %s([%s])", name, strings.Join(parts, ", "))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	}
	fatal("composite of", t)
	return ""
}

// compositeNeedsPin reports whether a keyed struct literal lists its fields out of declaration order while one of them has side effects.
func (g *gen) compositeNeedsPin(decl *compiler.StructDecl, e *compiler.Expr) bool {
	impure := false
	for _, f := range e.Fields {
		if compiler.Impure(f.Value) {
			impure = true
		}
	}
	if !impure {
		return false
	}
	position := map[string]int{}
	for i, f := range decl.Fields {
		position[f.Name] = i
	}
	for i := 1; i < len(e.Fields); i++ {
		if position[e.Fields[i].Name] < position[e.Fields[i-1].Name] {
			return true
		}
	}
	return false
}

// jsEscape writes bytes as a JavaScript string literal body where every character is one byte.
func jsEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			b.WriteString("\\\"")
		case c == '\\':
			b.WriteString("\\\\")
		case c == '\n':
			b.WriteString("\\n")
		case c < 32 || c > 126:
			fmt.Fprintf(&b, "\\x%02x", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
