// Package vegoc loads the Vego JSON form into a typed IR for the target-language printers.
// The checker in check.go gives every expression its Go type.
// The printers need those types for decisions the JSON leaves implicit: literal widths, signedness of division, shift operand casts, and local mutability.
package vegoc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
)

type TypeKind int

const (
	KBool TypeKind = iota
	KU8
	KU16
	KU32
	KU64
	KI32
	KI64
	KInt
	KStr
	KSlice
	KArray
	KStruct
	KPtr
	// KTuple is the synthetic type of a two-result call.
	KTuple
	// KNil is the type of the untyped nil constant, before context gives it a slice type.
	KNil
)

type Type struct {
	K    TypeKind
	Name string // struct name for KStruct and KPtr
	Elem *Type  // KSlice, KArray
	ALen *Expr  // KArray
	Tup  [2]*Type

	// Filled by the checker for KArray: the length value.
	ALenVal int64
	ALenSet bool
}

var (
	TBool = &Type{K: KBool}
	TU8   = &Type{K: KU8}
	TU16  = &Type{K: KU16}
	TU32  = &Type{K: KU32}
	TU64  = &Type{K: KU64}
	TI32  = &Type{K: KI32}
	TI64  = &Type{K: KI64}
	TInt  = &Type{K: KInt}
	TStr  = &Type{K: KStr}
)

func (t *Type) IsInteger() bool {
	switch t.K {
	case KU8, KU16, KU32, KU64, KI32, KI64, KInt:
		return true
	}
	return false
}

func (t *Type) Signed() bool {
	switch t.K {
	case KI32, KI64, KInt:
		return true
	}
	return false
}

func (t *Type) Width() int {
	switch t.K {
	case KU8:
		return 8
	case KU16:
		return 16
	case KU32, KI32:
		return 32
	default:
		return 64
	}
}

// Same reports structural type identity.
// KInt and KI64 stay distinct kinds, but they compare equal.
// They are the same 64-bit signed type in every target.
func Same(a, b *Type) bool {
	if a == nil || b == nil {
		return a == b
	}
	ka, kb := a.K, b.K
	if ka == KInt {
		ka = KI64
	}
	if kb == KInt {
		kb = KI64
	}
	if ka != kb {
		return false
	}
	switch a.K {
	case KSlice:
		return Same(a.Elem, b.Elem)
	case KArray:
		return Same(a.Elem, b.Elem) && sameLen(a, b)
	case KStruct, KPtr:
		return a.Name == b.Name
	}
	return true
}

// sameLen compares array length types by the value the checker resolved.
// A length the checker could not fold falls back to structural identity.
func sameLen(a, b *Type) bool {
	if a.ALenSet && b.ALenSet {
		return a.ALenVal == b.ALenVal
	}
	return sameLenExpr(a.ALen, b.ALen)
}

func sameLenExpr(a, b *Expr) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.K != b.K {
		return false
	}
	switch a.K {
	case "int":
		return a.Value == b.Value
	case "ident":
		return a.Name == b.Name
	case "binary":
		return a.Op == b.Op && sameLenExpr(a.X, b.X) && sameLenExpr(a.Y, b.Y)
	case "unary":
		return a.Op == b.Op && sameLenExpr(a.X, b.X)
	}
	return false
}

// ArrayLen returns the array length the checker resolved.
func (p *Program) ArrayLen(t *Type) (int64, bool) {
	return t.ALenVal, t.ALenSet
}

func (t *Type) String() string {
	switch t.K {
	case KBool:
		return "bool"
	case KU8:
		return "uint8"
	case KU16:
		return "uint16"
	case KU32:
		return "uint32"
	case KU64:
		return "uint64"
	case KI32:
		return "int32"
	case KI64:
		return "int64"
	case KInt:
		return "int"
	case KStr:
		return "string"
	case KSlice:
		return "[]" + t.Elem.String()
	case KArray:
		return "[...]" + t.Elem.String()
	case KStruct:
		return t.Name
	case KPtr:
		return "*" + t.Name
	case KTuple:
		return "(" + t.Tup[0].String() + ", " + t.Tup[1].String() + ")"
	}
	return "?"
}

type Expr struct {
	K       string // int char str bool ident field index slice_expr call builtin conv unary binary composite
	Value   string // int/char literal (decimal), str literal bytes
	BoolVal bool
	Name    string // ident name, field name, call/builtin function
	X       *Expr
	Y       *Expr
	Index   *Expr
	Lo, Hi  *Expr
	Args    []*Expr
	Elems   []*Expr
	Fields  []CompositeField
	Op      string
	Spread  bool
	TypeRef *Type // conv target, make type

	// Filled by the checker.
	Typ     *Type
	Untyped bool // constant expression whose type came from a default
	IsConst bool // compile-time constant expression
}

type CompositeField struct {
	Name  string
	Value *Expr
}

type SwitchCase struct {
	Values []*Expr
	Body   []*Stmt
}

type Stmt struct {
	K       string // var_decl define assign op_assign if for range switch break continue return expr_stmt block
	Name    string // var_decl
	Names   []string
	TypeRef *Type // var_decl declared or inferred type
	Value   *Expr
	Lhs     []*Expr
	Op      string
	Cond    *Expr
	Init    *Stmt
	Post    *Stmt
	Body    []*Stmt
	Then    []*Stmt
	Else    []*Stmt // nil when absent
	HasElse bool
	Tag     *Expr
	Cases   []SwitchCase
	Default []*Stmt // nil when absent
	HasDef  bool
	Values  []*Expr // return
	IdxName string  // range
	ValName string  // range

	// Filled by the checker: the type of each name a define declares, aligned with Names.
	DeclaredTypes []*Type
}

type Param struct {
	Name string
	Type *Type
}

type ValueDecl struct {
	Name  string
	Type  *Type // nil when the source had none
	Value *Expr

	// Filled by the checker.
	Inferred *Type    // declared type, or the default type
	ConstVal *big.Int // folded value for integer constants
}

type StructDecl struct {
	Name   string
	Fields []Param
}

type LocalInfo struct {
	Used    bool
	Mutated bool
}

type FuncDecl struct {
	Name    string
	Params  []Param
	Results []*Type
	Body    []*Stmt

	// Filled by the checker, keyed by post-rename local or parameter name.
	// The names are unique per function.
	Info map[string]*LocalInfo

	// Filled by the checker: the function allocates, directly or through a callee.
	// The printers give it the synthetic memory context parameter "mem".
	Allocates bool
}

type Program struct {
	Package string
	Consts  []*ValueDecl
	Vars    []*ValueDecl
	Types   []*StructDecl
	Funcs   []*FuncDecl

	ConstMap  map[string]*ValueDecl
	VarMap    map[string]*ValueDecl
	StructMap map[string]*StructDecl
	FuncMap   map[string]*FuncDecl
}

// WalkExpr calls fn on e and on every expression below it.
func WalkExpr(e *Expr, fn func(*Expr)) {
	if e == nil {
		return
	}
	fn(e)
	for _, sub := range []*Expr{e.X, e.Y, e.Index, e.Lo, e.Hi} {
		WalkExpr(sub, fn)
	}
	for _, a := range e.Args {
		WalkExpr(a, fn)
	}
	for _, el := range e.Elems {
		WalkExpr(el, fn)
	}
	for _, f := range e.Fields {
		WalkExpr(f.Value, fn)
	}
}

// WalkStmt calls fs on s and every statement below it, and fe on every expression they hold.
// Either callback may be nil.
func WalkStmt(s *Stmt, fe func(*Expr), fs func(*Stmt)) {
	if s == nil {
		return
	}
	if fs != nil {
		fs(s)
	}
	if fe != nil {
		for _, e := range []*Expr{s.Value, s.Cond, s.Tag} {
			WalkExpr(e, fe)
		}
		for _, l := range s.Lhs {
			WalkExpr(l, fe)
		}
		for _, v := range s.Values {
			WalkExpr(v, fe)
		}
		for _, cs := range s.Cases {
			for _, v := range cs.Values {
				WalkExpr(v, fe)
			}
		}
	}
	WalkStmt(s.Init, fe, fs)
	WalkStmt(s.Post, fe, fs)
	WalkBody(s.Body, fe, fs)
	WalkBody(s.Then, fe, fs)
	WalkBody(s.Else, fe, fs)
	for _, cs := range s.Cases {
		WalkBody(cs.Body, fe, fs)
	}
	WalkBody(s.Default, fe, fs)
}

// WalkBody applies WalkStmt to every statement of a body.
func WalkBody(body []*Stmt, fe func(*Expr), fs func(*Stmt)) {
	for _, s := range body {
		WalkStmt(s, fe, fs)
	}
}

// Impure reports whether the evaluation of e can have a side effect, or see one.
// The forms are a function call, and a builtin that allocates or writes.
// The printers use it to decide when the evaluation order of a target language needs pinning.
// The pinned order matches the left-to-right rule of Go.
func Impure(e *Expr) bool {
	found := false
	WalkExpr(e, func(x *Expr) {
		if x.K == "call" {
			found = true
		}
		if x.K == "builtin" {
			switch x.Name {
			case "append", "make", "copy":
				found = true
			}
		}
	})
	return found
}

// AppendNeedsPin reports whether a non-spread append must evaluate its elements into temporaries first.
// The printers nest one append per element, so a later element could see an earlier write.
// Only a call, or a read of a slice or array with the same element type, can do that.
func AppendNeedsPin(e *Expr) bool {
	if e.K != "builtin" || e.Name != "append" || e.Spread || len(e.Args) < 3 {
		return false
	}
	elem := e.Args[0].Typ.Elem
	for _, a := range e.Args[2:] {
		if Impure(a) {
			return true
		}
		reads := false
		WalkExpr(a, func(x *Expr) {
			if x.K != "index" || x.X.Typ == nil {
				return
			}
			if (x.X.Typ.K == KSlice || x.X.Typ.K == KArray) && Same(x.X.Typ.Elem, elem) {
				reads = true
			}
		})
		if reads {
			return true
		}
	}
	return false
}

// LoadFile reads a Vego JSON file, loads it, and checks it.
// This is the full front end that every printer runs.
func LoadFile(path string) (*Program, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	p, err := Load(blob)
	if err != nil {
		return nil, err
	}
	if err := Check(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Mangle folds a type into an identifier fragment.
// The Zig and C++ printers name their shared two-result tuple structs with it.
// The fragments are target-neutral and prefix-free across checked result types.
func Mangle(t *Type) string {
	switch t.K {
	case KBool:
		return "bool"
	case KU8:
		return "u8"
	case KU16:
		return "u16"
	case KU32:
		return "u32"
	case KU64:
		return "u64"
	case KI32:
		return "i32"
	case KI64, KInt:
		return "i64"
	case KStr:
		return "Str"
	case KSlice:
		return "s" + Mangle(t.Elem)
	case KArray:
		if !t.ALenSet {
			panic("cannot mangle an unresolved array length")
		}
		return "a" + strconv.FormatInt(t.ALenVal, 10) + "x" + Mangle(t.Elem)
	case KStruct:
		return "t" + hex.EncodeToString([]byte(t.Name)) + "x"
	}
	panic("cannot mangle type " + t.String())
}

const tupleNamePrefix = "Tup_"

// TupName names the struct type holding a two-result pair.
func TupName(pair [2]*Type) string {
	return tupleNamePrefix + Mangle(pair[0]) + "_" + Mangle(pair[1])
}

type object = map[string]any

func Load(blob []byte) (p *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			p = nil
			err = fmt.Errorf("invalid Vego JSON: %v", r)
		}
	}()
	var doc object
	if err := json.Unmarshal(blob, &doc); err != nil {
		return nil, err
	}
	version, ok := doc["vego"].(float64)
	if !ok {
		return nil, fmt.Errorf("invalid Vego JSON version")
	}
	if version != 1 {
		return nil, fmt.Errorf("unsupported Vego JSON version %v", version)
	}
	packageName, ok := doc["package"].(string)
	if !ok || packageName == "" {
		return nil, fmt.Errorf("invalid Vego JSON package")
	}
	consts, err := topLevelList(doc, "consts")
	if err != nil {
		return nil, err
	}
	vars, err := topLevelList(doc, "vars")
	if err != nil {
		return nil, err
	}
	types, err := topLevelList(doc, "types")
	if err != nil {
		return nil, err
	}
	funcs, err := topLevelList(doc, "funcs")
	if err != nil {
		return nil, err
	}
	p = &Program{
		Package:   packageName,
		ConstMap:  map[string]*ValueDecl{},
		VarMap:    map[string]*ValueDecl{},
		StructMap: map[string]*StructDecl{},
		FuncMap:   map[string]*FuncDecl{},
	}
	for _, d := range consts {
		v := loadValueDecl(d.(object))
		p.Consts = append(p.Consts, v)
		p.ConstMap[v.Name] = v
	}
	for _, d := range vars {
		v := loadValueDecl(d.(object))
		p.Vars = append(p.Vars, v)
		p.VarMap[v.Name] = v
	}
	for _, d := range types {
		o := d.(object)
		s := &StructDecl{Name: o["name"].(string)}
		for _, f := range o["fields"].([]any) {
			fo := f.(object)
			s.Fields = append(s.Fields, Param{Name: fo["name"].(string), Type: loadType(fo["type"])})
		}
		p.Types = append(p.Types, s)
		p.StructMap[s.Name] = s
	}
	for _, d := range funcs {
		o := d.(object)
		f := &FuncDecl{Name: o["name"].(string), Info: map[string]*LocalInfo{}}
		for _, pr := range o["params"].([]any) {
			po := pr.(object)
			f.Params = append(f.Params, Param{Name: po["name"].(string), Type: loadType(po["type"])})
		}
		for _, r := range o["results"].([]any) {
			f.Results = append(f.Results, loadType(r))
		}
		f.Body = loadBody(o["body"])
		p.Funcs = append(p.Funcs, f)
		p.FuncMap[f.Name] = f
	}
	return p, nil
}

func topLevelList(doc object, name string) ([]any, error) {
	values, ok := doc[name].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid Vego JSON %s list", name)
	}
	return values, nil
}

func loadValueDecl(o object) *ValueDecl {
	v := &ValueDecl{Name: o["name"].(string), Value: loadExpr(o["value"])}
	if o["type"] != nil {
		v.Type = loadType(o["type"])
	}
	return v
}

func loadType(t any) *Type {
	o := t.(object)
	switch o["k"].(string) {
	case "named":
		switch o["name"].(string) {
		case "bool":
			return TBool
		case "uint8":
			return TU8
		case "uint16":
			return TU16
		case "uint32":
			return TU32
		case "uint64":
			return TU64
		case "int32":
			return TI32
		case "int64":
			return TI64
		case "int":
			return TInt
		case "string":
			return TStr
		}
		panic(fmt.Sprintf("unknown named type %v", o["name"]))
	case "slice":
		return &Type{K: KSlice, Elem: loadType(o["elem"])}
	case "array":
		return &Type{K: KArray, Elem: loadType(o["elem"]), ALen: loadExpr(o["len"])}
	case "struct_ref":
		return &Type{K: KStruct, Name: o["name"].(string)}
	case "ptr":
		return &Type{K: KPtr, Name: o["name"].(string)}
	}
	panic(fmt.Sprintf("unknown type kind %v", o["k"]))
}

func loadBody(b any) []*Stmt {
	if b == nil {
		return nil
	}
	var out []*Stmt
	for _, s := range b.([]any) {
		out = append(out, loadStmt(s.(object)))
	}
	return out
}

func loadStmt(o object) *Stmt {
	s := &Stmt{K: o["k"].(string)}
	switch s.K {
	case "var_decl":
		s.Name = o["name"].(string)
		if o["type"] != nil {
			s.TypeRef = loadType(o["type"])
		}
		if o["value"] != nil {
			s.Value = loadExpr(o["value"])
		}
	case "define":
		for _, n := range o["names"].([]any) {
			s.Names = append(s.Names, n.(string))
		}
		s.Value = loadExpr(o["value"])
	case "assign":
		for _, l := range o["lhs"].([]any) {
			s.Lhs = append(s.Lhs, loadExpr(l))
		}
		s.Value = loadExpr(o["value"])
	case "op_assign":
		s.Op = o["op"].(string)
		s.Lhs = []*Expr{loadExpr(o["lhs"])}
		s.Value = loadExpr(o["value"])
	case "incdec":
		// x++ and x-- become x += 1 and x -= 1.
		// The operand is a place expression, so the rewrite is exact.
		s.K = "op_assign"
		if o["op"].(string) == "++" {
			s.Op = "+="
		} else {
			s.Op = "-="
		}
		s.Lhs = []*Expr{loadExpr(o["lhs"])}
		s.Value = &Expr{K: "int", Value: "1"}
	case "if":
		s.Cond = loadExpr(o["cond"])
		s.Then = loadBody(o["then"])
		if o["else"] != nil {
			s.Else = loadBody(o["else"])
			s.HasElse = true
		}
	case "for":
		if o["init"] != nil {
			s.Init = loadStmt(o["init"].(object))
		}
		if o["cond"] != nil {
			s.Cond = loadExpr(o["cond"])
		}
		if o["post"] != nil {
			s.Post = loadStmt(o["post"].(object))
		}
		s.Body = loadBody(o["body"])
	case "range":
		if o["idx"] != nil {
			s.IdxName = o["idx"].(string)
		}
		if o["val"] != nil {
			s.ValName = o["val"].(string)
		}
		s.Value = loadExpr(o["over"])
		s.Body = loadBody(o["body"])
	case "switch":
		s.Tag = loadExpr(o["tag"])
		for _, raw := range o["cases"].([]any) {
			co := raw.(object)
			var c SwitchCase
			for _, v := range co["values"].([]any) {
				c.Values = append(c.Values, loadExpr(v))
			}
			c.Body = loadBody(co["body"])
			s.Cases = append(s.Cases, c)
		}
		if o["default"] != nil {
			s.Default = loadBody(o["default"])
			s.HasDef = true
		}
	case "break", "continue":
	case "return":
		for _, v := range o["values"].([]any) {
			s.Values = append(s.Values, loadExpr(v))
		}
	case "expr_stmt":
		s.Value = loadExpr(o["value"])
	case "block":
		s.Body = loadBody(o["body"])
	default:
		panic("unknown statement kind " + s.K)
	}
	return s
}

func loadExpr(e any) *Expr {
	o := e.(object)
	x := &Expr{K: o["k"].(string)}
	switch x.K {
	case "int", "char":
		x.Value = o["value"].(string)
	case "str":
		x.Value = o["value"].(string)
	case "bool":
		x.BoolVal = o["value"].(bool)
	case "ident":
		x.Name = o["name"].(string)
	case "field":
		x.X = loadExpr(o["x"])
		x.Name = o["name"].(string)
	case "index":
		x.X = loadExpr(o["x"])
		x.Index = loadExpr(o["index"])
	case "slice_expr":
		x.X = loadExpr(o["x"])
		if o["lo"] != nil {
			x.Lo = loadExpr(o["lo"])
		}
		if o["hi"] != nil {
			x.Hi = loadExpr(o["hi"])
		}
	case "call":
		x.Name = o["fn"].(string)
		for _, a := range o["args"].([]any) {
			x.Args = append(x.Args, loadExpr(a))
		}
	case "builtin":
		x.Name = o["fn"].(string)
		for _, a := range o["args"].([]any) {
			x.Args = append(x.Args, loadExpr(a))
		}
		if sp, ok := o["spread"].(bool); ok {
			x.Spread = sp
		}
		if o["type"] != nil {
			x.TypeRef = loadType(o["type"])
		}
	case "conv":
		x.TypeRef = loadType(o["type"])
		x.X = loadExpr(o["x"])
	case "unary":
		x.Op = o["op"].(string)
		x.X = loadExpr(o["x"])
	case "binary":
		x.Op = o["op"].(string)
		x.X = loadExpr(o["x"])
		x.Y = loadExpr(o["y"])
	case "composite":
		x.TypeRef = loadType(o["type"])
		if fs, ok := o["fields"]; ok && fs != nil {
			for _, f := range fs.([]any) {
				fo := f.(object)
				x.Fields = append(x.Fields, CompositeField{Name: fo["name"].(string), Value: loadExpr(fo["value"])})
			}
		} else {
			for _, el := range o["elems"].([]any) {
				x.Elems = append(x.Elems, loadExpr(el))
			}
		}
	default:
		panic("unknown expression kind " + x.K)
	}
	return x
}
