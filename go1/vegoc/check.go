package vegoc

import (
	"fmt"
	"math/big"
)

// Check resolves the type of every expression, folds constant
// values, renames locals so every name is unique inside its
// function (and never shadows a package-level name or a runtime
// name), and records which locals are used and mutated. It must
// run once, right after Load.
func Check(p *Program) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("vegoc: %v", r)
		}
	}()
	c := &checker{p: p}
	c.run()
	return nil
}

type local struct {
	unique string
	typ    *Type
	info   *LocalInfo
}

type scope struct {
	parent *scope
	vars   map[string]*local
}

func (s *scope) lookup(name string) *local {
	for sc := s; sc != nil; sc = sc.parent {
		if l, ok := sc.vars[name]; ok {
			return l
		}
	}
	return nil
}

type checker struct {
	p     *Program
	fn    *FuncDecl
	scope *scope
	// taken holds every name already claimed inside the current
	// function, so renames stay unique across sibling scopes.
	taken map[string]bool
}

// reserved names the runtimes of the targets claim. "mem" is the
// memory context every allocating function receives; a program
// must leave the name free at the package level, and locals of
// that name get renamed like any other clash.
var reserved = map[string]bool{
	"vg": true, "std": true, "Str": true, "Slice": true,
	"self": true, "Self": true, "crate": true, "super": true,
	"mem": true,
}

func (c *checker) run() {
	c.checkReservedPackageNames()
	for _, d := range c.p.Consts {
		c.ensureConst(d)
	}
	c.resolveArrayLens()
	for _, d := range c.p.Vars {
		c.checkVarDecl(d)
	}
	for _, f := range c.p.Funcs {
		c.checkFunc(f)
	}
	for _, f := range c.p.Funcs {
		c.rewriteMutatedParams(f)
	}
	markAllocates(c.p)
}

// checkReservedPackageNames rejects package-level declarations
// whose name a target runtime claims. Locals get renamed instead;
// a package-level name reaches every target verbatim, so it must
// stay clear.
func (c *checker) checkReservedPackageNames() {
	check := func(kind, name string) {
		if reserved[name] {
			panic(fmt.Sprintf("%s %s: the name is reserved for the target runtimes", kind, name))
		}
	}
	for _, d := range c.p.Consts {
		check("const", d.Name)
	}
	for _, d := range c.p.Vars {
		check("var", d.Name)
	}
	for _, s := range c.p.Types {
		check("type", s.Name)
	}
	for _, f := range c.p.Funcs {
		check("func", f.Name)
	}
}

func (c *checker) packageName(name string) bool {
	if reserved[name] {
		return true
	}
	if _, ok := c.p.ConstMap[name]; ok {
		return true
	}
	if _, ok := c.p.VarMap[name]; ok {
		return true
	}
	if _, ok := c.p.StructMap[name]; ok {
		return true
	}
	if _, ok := c.p.FuncMap[name]; ok {
		return true
	}
	return false
}

// ensureConst checks a constant declaration on demand, so constants
// may reference each other in any source order.
func (c *checker) ensureConst(d *ValueDecl) {
	if d.Inferred != nil {
		return
	}
	c.inferDecl(d)
	if d.Inferred.IsInteger() {
		d.ConstVal = c.fold(d.Value)
	}
}

// inferDecl types a declaration's value against its declared type,
// or freezes the value's default type.
func (c *checker) inferDecl(d *ValueDecl) {
	c.checkExpr(d.Value)
	if d.Type != nil {
		c.retype(d.Value, d.Type)
		d.Inferred = d.Type
	} else {
		c.defaultType(d.Value)
		d.Inferred = d.Value.Typ
	}
}

// resolveArrayLens folds every array type's length against the
// checked constants, so Same can compare lengths by value and the
// printers can zero-pad partial array literals.
func (c *checker) resolveArrayLens() {
	var resolveType func(t *Type)
	resolveType = func(t *Type) {
		if t == nil {
			return
		}
		switch t.K {
		case KSlice:
			resolveType(t.Elem)
		case KArray:
			resolveType(t.Elem)
			if !t.ALenSet {
				if v, ok := c.tryFold(t.ALen); ok {
					t.ALenVal, t.ALenSet = v, true
				}
			}
		}
	}
	fe := func(e *Expr) { resolveType(e.TypeRef) }
	fs := func(s *Stmt) { resolveType(s.TypeRef) }
	for _, d := range c.p.Consts {
		resolveType(d.Type)
	}
	for _, d := range c.p.Vars {
		resolveType(d.Type)
		WalkExpr(d.Value, fe)
	}
	for _, sd := range c.p.Types {
		for _, f := range sd.Fields {
			resolveType(f.Type)
		}
	}
	for _, f := range c.p.Funcs {
		for _, pa := range f.Params {
			resolveType(pa.Type)
		}
		for _, r := range f.Results {
			resolveType(r)
		}
		WalkBody(f.Body, fe, fs)
	}
}

// tryFold folds an integer constant expression, reporting failure
// instead of panicking.
func (c *checker) tryFold(e *Expr) (v int64, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f := c.fold(e)
	if !f.IsInt64() {
		return 0, false
	}
	return f.Int64(), true
}

func (c *checker) checkVarDecl(d *ValueDecl) {
	c.inferDecl(d)
	// cmd/vego2json enforces the same rules at the Go front end,
	// with source positions; this guards other JSON producers.
	if c.typeContainsSlice(d.Inferred) {
		panic("package variable " + d.Name + " contains a slice; globals are static constant data")
	}
	// Globals are static constant data, so the initializer must
	// not allocate or call: there is no memory context and no
	// evaluation order at package scope.
	WalkExpr(d.Value, func(e *Expr) {
		if ExprAllocates(e) || e.K == "call" {
			panic("package variable " + d.Name + " has a non-constant initializer")
		}
	})
}

func (c *checker) typeContainsSlice(t *Type) bool {
	switch t.K {
	case KSlice:
		return true
	case KArray:
		return c.typeContainsSlice(t.Elem)
	case KStruct:
		for _, f := range c.p.StructMap[t.Name].Fields {
			if c.typeContainsSlice(f.Type) {
				return true
			}
		}
	}
	return false
}

func (c *checker) checkFunc(f *FuncDecl) {
	c.fn = f
	c.taken = map[string]bool{}
	c.scope = &scope{vars: map[string]*local{}}
	for i := range f.Params {
		pa := &f.Params[i]
		l := c.declare(pa.Name, pa.Type)
		pa.Name = l.unique
	}
	c.checkBody(f.Body)
	c.scope = nil
	c.fn = nil
}

// declare adds a local, renaming it if the name is already taken in
// this function or clashes with a package-level or reserved name.
func (c *checker) declare(name string, t *Type) *local {
	unique := name
	if name != "_" {
		if c.taken[unique] || c.packageName(unique) {
			for i := 2; ; i++ {
				cand := fmt.Sprintf("%s_%d", name, i)
				if !c.taken[cand] && !c.packageName(cand) {
					unique = cand
					break
				}
			}
		}
		c.taken[unique] = true
	}
	l := &local{unique: unique, typ: t, info: &LocalInfo{}}
	if name != "_" {
		c.scope.vars[name] = l
		c.fn.Info[unique] = l.info
	}
	return l
}

func (c *checker) push() { c.scope = &scope{parent: c.scope, vars: map[string]*local{}} }
func (c *checker) pop()  { c.scope = c.scope.parent }

func (c *checker) checkBody(body []*Stmt) {
	c.push()
	for _, s := range body {
		c.checkStmt(s)
	}
	c.pop()
}

func (c *checker) checkStmt(s *Stmt) {
	switch s.K {
	case "var_decl":
		if s.Value != nil {
			c.checkExpr(s.Value)
			if s.TypeRef != nil {
				c.retype(s.Value, s.TypeRef)
			} else {
				c.defaultType(s.Value)
				s.TypeRef = s.Value.Typ
			}
		}
		l := c.declare(s.Name, s.TypeRef)
		s.Name = l.unique
	case "define":
		t := c.checkExpr(s.Value)
		if len(s.Names) == 1 {
			c.defaultType(s.Value)
			l := c.declare(s.Names[0], s.Value.Typ)
			s.Names[0] = l.unique
			s.DeclaredTypes = []*Type{s.Value.Typ}
		} else {
			if t == nil || t.K != KTuple {
				panic("two-name define needs a two-result call")
			}
			for i, n := range s.Names {
				l := c.declare(n, t.Tup[i])
				s.Names[i] = l.unique
				s.DeclaredTypes = append(s.DeclaredTypes, t.Tup[i])
			}
		}
	case "assign":
		if len(s.Lhs) == 2 {
			t := c.checkExpr(s.Value)
			if t == nil || t.K != KTuple {
				panic("two-value assign needs a two-result call")
			}
			for i, l := range s.Lhs {
				if l.K == "ident" && l.Name == "_" {
					l.Typ = t.Tup[i]
					continue
				}
				lt := c.checkPlace(l)
				if !Same(lt, t.Tup[i]) {
					panic("two-value assign type mismatch")
				}
				// Every printer evaluates the call before the
				// places; Go evaluates the places first, so a
				// place with side effects cannot translate.
				if Impure(l) {
					panic("two-value assign place with side effects is not supported")
				}
				c.markMutated(l)
			}
			return
		}
		l := s.Lhs[0]
		if l.K == "ident" && l.Name == "_" {
			c.checkExpr(s.Value)
			c.defaultType(s.Value)
			l.Typ = s.Value.Typ
			return
		}
		lt := c.checkPlace(l)
		c.markMutated(l)
		c.checkExpr(s.Value)
		c.retype(s.Value, lt)
	case "op_assign":
		lt := c.checkPlace(s.Lhs[0])
		c.markMutated(s.Lhs[0])
		c.checkExpr(s.Value)
		if s.Op == ">>=" || s.Op == "<<=" {
			if s.Value.Untyped {
				c.retype(s.Value, TInt)
			}
		} else {
			c.retype(s.Value, lt)
		}
	case "if":
		c.checkExpr(s.Cond)
		c.retype(s.Cond, TBool)
		c.checkBody(s.Then)
		if s.HasElse {
			c.checkBody(s.Else)
		}
	case "for":
		c.push()
		if s.Init != nil {
			c.checkStmt(s.Init)
		}
		if s.Cond != nil {
			c.checkExpr(s.Cond)
			c.retype(s.Cond, TBool)
		}
		// The body runs before the post statement, and the body
		// scope must not capture the post statement's names.
		c.checkBody(s.Body)
		if s.Post != nil {
			c.checkStmt(s.Post)
		}
		c.pop()
	case "range":
		c.checkExpr(s.Value)
		c.defaultType(s.Value)
		t := s.Value.Typ
		c.push()
		var elem *Type
		switch t.K {
		case KSlice, KArray:
			elem = t.Elem
		case KInt, KI64:
		default:
			panic("range over unsupported type " + t.String())
		}
		// The lowered loops copy a hidden counter into the user
		// variables each iteration, so their mutability follows
		// the body alone.
		if s.IdxName != "" && s.IdxName != "_" {
			l := c.declare(s.IdxName, TInt)
			s.IdxName = l.unique
		}
		if s.ValName != "" && s.ValName != "_" {
			if elem == nil {
				panic("range value over int count")
			}
			l := c.declare(s.ValName, elem)
			s.ValName = l.unique
		}
		c.checkBody(s.Body)
		c.pop()
	case "switch":
		c.checkExpr(s.Tag)
		c.defaultType(s.Tag)
		tt := s.Tag.Typ
		for _, cs := range s.Cases {
			for _, v := range cs.Values {
				c.checkExpr(v)
				c.retype(v, tt)
			}
			c.checkBody(cs.Body)
		}
		if s.HasDef {
			c.checkBody(s.Default)
		}
	case "break", "continue":
	case "return":
		if len(s.Values) == 1 && len(c.fn.Results) == 2 {
			// return f(...) forwarding both results.
			t := c.checkExpr(s.Values[0])
			if t == nil || t.K != KTuple || !Same(t.Tup[0], c.fn.Results[0]) || !Same(t.Tup[1], c.fn.Results[1]) {
				panic("bad two-result forward")
			}
			return
		}
		for i, v := range s.Values {
			c.checkExpr(v)
			c.retype(v, c.fn.Results[i])
		}
	case "expr_stmt":
		c.checkExpr(s.Value)
		c.defaultType(s.Value)
	case "block":
		c.checkBody(s.Body)
	default:
		panic("unknown statement kind " + s.K)
	}
}

// checkPlace types an assignable expression.
func (c *checker) checkPlace(e *Expr) *Type {
	t := c.checkExpr(e)
	if e.Untyped {
		panic("constant as assignment target")
	}
	return t
}

// markMutated records that an assignment writes into the storage of
// a local. Writes that pass through a slice element or a pointer
// parameter land outside the local's own storage and do not count.
// Identifier names were already rewritten to their unique form.
func (c *checker) markMutated(e *Expr) {
	if c.fn == nil {
		return
	}
	for {
		switch e.K {
		case "ident":
			if info, ok := c.fn.Info[e.Name]; ok {
				info.Mutated = true
			}
			return
		case "field":
			if e.X.Typ != nil && e.X.Typ.K == KPtr {
				return
			}
			e = e.X
		case "index":
			if e.X.Typ != nil && e.X.Typ.K == KArray {
				e = e.X
				continue
			}
			return
		default:
			return
		}
	}
}

// checkExpr computes e.Typ bottom-up. For a constant expression it
// sets Untyped and stores the default type; a parent then either
// retypes it against context or leaves the default.
func (c *checker) checkExpr(e *Expr) *Type {
	switch e.K {
	case "int":
		e.Typ, e.Untyped = TInt, true
	case "char":
		e.Typ, e.Untyped = TI32, true
	case "str":
		e.Typ, e.Untyped = TStr, true
	case "bool":
		e.Typ, e.Untyped = TBool, true
	case "ident":
		if e.Name == "nil" {
			e.Typ = &Type{K: KNil}
			e.Untyped = true
			return e.Typ
		}
		if l := c.lookupLocal(e); l != nil {
			l.info.Used = true
			e.Name = l.unique
			e.Typ = l.typ
		} else if d, ok := c.p.ConstMap[e.Name]; ok {
			c.ensureConst(d)
			e.Typ = d.Inferred
			e.Untyped = d.Type == nil
		} else if d, ok := c.p.VarMap[e.Name]; ok {
			e.Typ = d.Inferred
		} else {
			panic("unknown identifier " + e.Name)
		}
	case "field":
		xt := c.checkExpr(e.X)
		name := xt.Name
		if xt.K != KStruct && xt.K != KPtr {
			panic("field access on " + xt.String())
		}
		sd := c.p.StructMap[name]
		var ft *Type
		for _, f := range sd.Fields {
			if f.Name == e.Name {
				ft = f.Type
			}
		}
		if ft == nil {
			panic("unknown field " + name + "." + e.Name)
		}
		e.Typ = ft
	case "index":
		xt := c.checkExpr(e.X)
		c.checkIndexOperand(e.Index)
		switch xt.K {
		case KSlice, KArray:
			e.Typ = xt.Elem
		case KStr:
			e.Typ = TU8
		default:
			panic("index of " + xt.String())
		}
	case "slice_expr":
		xt := c.checkExpr(e.X)
		if e.Lo != nil {
			c.checkIndexOperand(e.Lo)
		}
		if e.Hi != nil {
			c.checkIndexOperand(e.Hi)
		}
		switch xt.K {
		case KSlice:
			e.Typ = xt
		case KArray:
			e.Typ = &Type{K: KSlice, Elem: xt.Elem}
			// Slicing an array takes the address of its storage.
			c.markMutated(e.X)
		case KStr:
			e.Typ = TStr
		default:
			panic("slice of " + xt.String())
		}
	case "call":
		f, ok := c.p.FuncMap[e.Name]
		if !ok {
			panic("unknown function " + e.Name)
		}
		if len(e.Args) != len(f.Params) {
			panic("bad argument count for " + e.Name)
		}
		for i, a := range e.Args {
			c.checkExpr(a)
			c.retype(a, f.Params[i].Type)
		}
		switch len(f.Results) {
		case 0:
			e.Typ = nil
		case 1:
			e.Typ = f.Results[0]
		case 2:
			e.Typ = &Type{K: KTuple, Tup: [2]*Type{f.Results[0], f.Results[1]}}
		}
	case "builtin":
		c.checkBuiltin(e)
	case "conv":
		c.checkExpr(e.X)
		if e.X.Untyped {
			// A constant conversion: the operand takes the target
			// type directly. Go guarantees the value fits.
			c.retype(e.X, e.TypeRef)
		}
		e.Typ = e.TypeRef
	case "unary":
		switch e.Op {
		case "-", "^":
			c.checkExpr(e.X)
			e.Typ = e.X.Typ
			e.Untyped = e.X.Untyped
		case "!":
			c.checkExpr(e.X)
			c.retype(e.X, TBool)
			e.Typ = TBool
		case "&":
			xt := c.checkExpr(e.X)
			if xt.K != KStruct {
				panic("& on non-struct " + xt.String())
			}
			c.markMutated(e.X)
			e.Typ = &Type{K: KPtr, Name: xt.Name}
		default:
			panic("unknown unary op " + e.Op)
		}
	case "binary":
		c.checkBinary(e)
	case "composite":
		e.Typ = e.TypeRef
		switch e.TypeRef.K {
		case KStruct:
			sd := c.p.StructMap[e.TypeRef.Name]
			for _, f := range e.Fields {
				var ft *Type
				for _, sf := range sd.Fields {
					if sf.Name == f.Name {
						ft = sf.Type
					}
				}
				if ft == nil {
					panic("unknown composite field " + f.Name)
				}
				c.checkExpr(f.Value)
				c.retype(f.Value, ft)
			}
		case KSlice, KArray:
			for _, el := range e.Elems {
				c.checkExpr(el)
				c.retype(el, e.TypeRef.Elem)
			}
		default:
			panic("composite of " + e.TypeRef.String())
		}
	default:
		panic("unknown expression kind " + e.K)
	}
	c.markConst(e)
	return e.Typ
}

// markConst flags compile-time constant expressions. Printers use
// the flag for literal emission decisions (Zig comptime contexts,
// C++ literal suffixes).
func (c *checker) markConst(e *Expr) {
	switch e.K {
	case "int", "char", "str", "bool":
		e.IsConst = true
	case "ident":
		// Locals never share a package-level name: declare renames
		// them, and checkExpr rewrote this ident already.
		_, e.IsConst = c.p.ConstMap[e.Name]
	case "unary":
		e.IsConst = e.Op != "&" && e.X.IsConst
	case "binary":
		e.IsConst = e.X.IsConst && e.Y.IsConst
	case "conv":
		e.IsConst = e.TypeRef.IsInteger() && e.X.IsConst
	case "builtin":
		if e.Name == "min" || e.Name == "max" {
			e.IsConst = e.Args[0].IsConst && e.Args[1].IsConst
		}
	}
}

func (c *checker) lookupLocal(e *Expr) *local {
	if c.scope == nil {
		return nil
	}
	return c.scope.lookup(e.Name)
}

func (c *checker) checkIndexOperand(e *Expr) {
	c.checkExpr(e)
	if e.Untyped {
		c.retype(e, TInt)
	}
	if !e.Typ.IsInteger() {
		panic("non-integer index " + e.Typ.String())
	}
}

func (c *checker) checkBuiltin(e *Expr) {
	switch e.Name {
	case "len", "cap":
		c.checkExpr(e.Args[0])
		e.Typ = TInt
	case "make":
		for _, a := range e.Args {
			c.checkIndexOperand(a)
		}
		e.Typ = e.TypeRef
	case "append":
		st := c.checkExpr(e.Args[0])
		if st.K != KSlice {
			panic("append to " + st.String())
		}
		for _, a := range e.Args[1:] {
			c.checkExpr(a)
			if e.Spread {
				if a.Typ.K == KStr {
					c.retype(a, TStr)
				} else {
					c.retype(a, st)
				}
			} else {
				c.retype(a, st.Elem)
			}
		}
		e.Typ = st
	case "copy":
		dt := c.checkExpr(e.Args[0])
		c.checkExpr(e.Args[1])
		if dt.K != KSlice {
			panic("copy into " + dt.String())
		}
		st := e.Args[1].Typ
		if st.K == KStr {
			c.retype(e.Args[1], TStr)
		} else {
			c.retype(e.Args[1], dt)
		}
		e.Typ = TInt
	case "min", "max":
		c.checkExpr(e.Args[0])
		c.checkExpr(e.Args[1])
		x, y := e.Args[0], e.Args[1]
		switch {
		case x.Untyped && !y.Untyped:
			c.retype(x, y.Typ)
		case !x.Untyped && y.Untyped:
			c.retype(y, x.Typ)
		case x.Untyped && y.Untyped:
			t := unifyDefaults(x.Typ, y.Typ)
			x.Typ = t
			y.Typ = t
			e.Untyped = true
		}
		if !Same(x.Typ, y.Typ) {
			panic("min/max type mismatch " + x.Typ.String() + " vs " + y.Typ.String())
		}
		e.Typ = x.Typ
	default:
		panic("unknown builtin " + e.Name)
	}
}

// unifyDefaults picks the default type of a constant operator pair:
// rune wins over int, like Go.
func unifyDefaults(a, b *Type) *Type {
	if a.K == KI32 || b.K == KI32 {
		return TI32
	}
	return a
}

func (c *checker) checkBinary(e *Expr) {
	c.checkExpr(e.X)
	c.checkExpr(e.Y)
	switch e.Op {
	case "&&", "||":
		c.retype(e.X, TBool)
		c.retype(e.Y, TBool)
		e.Typ = TBool
		e.Untyped = e.X.Untyped && e.Y.Untyped
	case "==", "!=", "<", "<=", ">", ">=":
		c.unifyOperands(e)
		e.Untyped = e.X.Untyped && e.Y.Untyped
		// Operands never take a type from above a comparison, so
		// freeze constant operands at their unified default now.
		c.defaultType(e.X)
		c.defaultType(e.Y)
		e.Typ = TBool
	case "<<", ">>":
		if e.Y.Untyped {
			c.retype(e.Y, TInt)
		}
		if !e.Y.Typ.IsInteger() {
			panic("non-integer shift count")
		}
		e.Typ = e.X.Typ
		e.Untyped = e.X.Untyped
	case "+", "-", "*", "/", "%", "&", "|", "^", "&^":
		c.unifyOperands(e)
		e.Typ = e.X.Typ
		e.Untyped = e.X.Untyped && e.Y.Untyped
	default:
		panic("unknown binary op " + e.Op)
	}
}

func (c *checker) unifyOperands(e *Expr) {
	x, y := e.X, e.Y
	switch {
	case x.Untyped && !y.Untyped:
		c.retype(x, y.Typ)
	case !x.Untyped && y.Untyped:
		c.retype(y, x.Typ)
	case x.Untyped && y.Untyped:
		t := unifyDefaults(x.Typ, y.Typ)
		x.Typ = t
		y.Typ = t
	default:
		if !Same(x.Typ, y.Typ) {
			panic(fmt.Sprintf("operand type mismatch: %s %s %s", x.Typ, e.Op, y.Typ))
		}
	}
}

// retype forces a concrete type onto an untyped constant
// expression, recursing into the constant's structure. On an
// already-typed expression it only cross-checks.
func (c *checker) retype(e *Expr, t *Type) {
	if t.K == KTuple {
		panic("tuple in value position")
	}
	if !e.Untyped {
		if e.Typ != nil && !Same(e.Typ, t) {
			panic(fmt.Sprintf("type mismatch: have %s, want %s (%s)", e.Typ, t, e.K))
		}
		return
	}
	e.Typ = t
	e.Untyped = false
	switch e.K {
	case "int", "char", "str", "bool", "ident":
	case "unary":
		c.retype(e.X, t)
	case "binary":
		switch e.Op {
		case "<<", ">>":
			c.retype(e.X, t)
		case "&&", "||", "==", "!=", "<", "<=", ">", ">=":
			// Comparison results are plain bools; operands keep
			// their own unified type.
			e.Typ = TBool
		default:
			c.retype(e.X, t)
			c.retype(e.Y, t)
		}
	case "builtin":
		if e.Name == "min" || e.Name == "max" {
			c.retype(e.Args[0], t)
			c.retype(e.Args[1], t)
		}
	default:
		panic("cannot retype " + e.K)
	}
}

// defaultType freezes an untyped constant at its default type.
func (c *checker) defaultType(e *Expr) {
	if !e.Untyped {
		return
	}
	t := e.Typ
	if t.K == KI32 {
		// A rune-defaulted constant stays int32.
		t = TI32
	}
	c.retype(e, t)
}

// fold evaluates an integer constant expression. It follows Go
// constant arithmetic (arbitrary precision, then the declared type
// bounds the value; the source compiled, so no truncation happens).
func (c *checker) fold(e *Expr) *big.Int {
	switch e.K {
	case "int", "char":
		v := new(big.Int)
		v.SetString(e.Value, 10)
		return v
	case "ident":
		if d, ok := c.p.ConstMap[e.Name]; ok {
			if d.ConstVal == nil {
				d.ConstVal = c.fold(d.Value)
			}
			return new(big.Int).Set(d.ConstVal)
		}
		panic("cannot fold identifier " + e.Name)
	case "unary":
		x := c.fold(e.X)
		switch e.Op {
		case "-":
			return x.Neg(x)
		case "^":
			return x.Not(x)
		}
	case "binary":
		x := c.fold(e.X)
		y := c.fold(e.Y)
		switch e.Op {
		case "+":
			return x.Add(x, y)
		case "-":
			return x.Sub(x, y)
		case "*":
			return x.Mul(x, y)
		case "/":
			return x.Quo(x, y)
		case "%":
			return x.Rem(x, y)
		case "<<":
			return x.Lsh(x, uint(y.Uint64()))
		case ">>":
			return x.Rsh(x, uint(y.Uint64()))
		case "&":
			return x.And(x, y)
		case "|":
			return x.Or(x, y)
		case "^":
			return x.Xor(x, y)
		case "&^":
			return x.AndNot(x, y)
		}
	case "conv":
		x := c.fold(e.X)
		return truncateTo(x, e.TypeRef)
	case "builtin":
		x := c.fold(e.Args[0])
		y := c.fold(e.Args[1])
		if e.Name == "min" {
			if x.Cmp(y) <= 0 {
				return x
			}
			return y
		}
		if e.Name == "max" {
			if x.Cmp(y) >= 0 {
				return x
			}
			return y
		}
	}
	panic("cannot fold constant expression " + e.K)
}

func truncateTo(v *big.Int, t *Type) *big.Int {
	w := uint(t.Width())
	mod := new(big.Int).Lsh(big.NewInt(1), w)
	v = new(big.Int).Mod(v, mod)
	if t.Signed() {
		half := new(big.Int).Lsh(big.NewInt(1), w-1)
		if v.Cmp(half) >= 0 {
			v.Sub(v, mod)
		}
	}
	return v
}

// rewriteMutatedParams gives every parameter that the body assigns
// a local shadow copy, so targets can keep parameters immutable.
func (c *checker) rewriteMutatedParams(f *FuncDecl) {
	var pre []*Stmt
	for i := range f.Params {
		pa := &f.Params[i]
		info := f.Info[pa.Name]
		if info == nil || !info.Mutated {
			continue
		}
		shadow := pa.Name + "_v"
		for f.Info[shadow] != nil {
			shadow += "v"
		}
		old := pa.Name
		renameIdents(f.Body, old, shadow)
		def := &Stmt{
			K:             "define",
			Names:         []string{shadow},
			Value:         &Expr{K: "ident", Name: old, Typ: pa.Type},
			DeclaredTypes: []*Type{pa.Type},
		}
		pre = append(pre, def)
		f.Info[shadow] = &LocalInfo{Used: true, Mutated: true}
		f.Info[old] = &LocalInfo{Used: true}
	}
	if len(pre) > 0 {
		f.Body = append(pre, f.Body...)
	}
}

func renameIdents(body []*Stmt, old, new string) {
	WalkBody(body, func(e *Expr) {
		if e.K == "ident" && e.Name == old {
			e.Name = new
		}
	}, nil)
}
