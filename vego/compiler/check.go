package compiler

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Check resolves the type of every expression and folds the constant values.
// It renames locals, so every name is unique inside its function.
// No local name hides a package-level name or a runtime name.
// It also records which locals the program reads and writes.
// It must run once, immediately after Load.
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
	p          *Program
	fn         *FuncDecl
	scope      *scope
	breakable  []bool
	loopDepth  int
	allowAddr  bool
	constState map[*ValueDecl]uint8
	// taken holds every name already claimed inside the current function.
	// Renames therefore stay unique across sibling scopes.
	taken map[string]bool
}

// reserved lists the names that the target runtimes claim.
// "mem" is the memory context every allocating function receives.
// A program must leave that name free at the package level.
// A local with that name renames like any other clash.
var reserved = map[string]bool{
	"vg": true, "std": true, "Str": true, "Slice": true,
	"self": true, "Self": true, "crate": true, "super": true,
	"mem": true,
}

func (c *checker) run() {
	c.constState = map[*ValueDecl]uint8{}
	c.checkDeclarationNames()
	c.checkTypes()
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
	checkViews(c.p)
	for _, f := range c.p.Funcs {
		c.rewriteMutatedParams(f)
	}
	markAllocates(c.p)
	checkEvalOrder(c.p)
}

func (c *checker) checkDeclarationNames() {
	checkNonBlankName("package", c.p.Package)
	checkIdentifier("package", c.p.Package)
	if ReservedPackageName(c.p.Package) {
		panic(fmt.Sprintf("package name %s is reserved for the target runtimes", c.p.Package))
	}
	claimed := map[string]string{}
	checkPackage := func(kind, name string) {
		checkDeclaredName(kind, name)
		if ReservedPackageName(name) {
			panic(fmt.Sprintf("%s %s: the name is reserved for the target runtimes", kind, name))
		}
		if prior := claimed[name]; prior != "" {
			panic(fmt.Sprintf("%s %s redeclares package %s", kind, name, prior))
		}
		claimed[name] = kind
	}
	for _, d := range c.p.Consts {
		checkPackage("const", d.Name)
	}
	for _, d := range c.p.Vars {
		checkPackage("var", d.Name)
	}
	for _, s := range c.p.Types {
		checkPackage("type", s.Name)
		if predeclaredTypeName(s.Name) {
			panic("type " + s.Name + " redeclares a predeclared type")
		}
		fields := map[string]bool{}
		for _, f := range s.Fields {
			checkNonBlankName("struct field", f.Name)
			checkIdentifier("struct field", f.Name)
			if fields[f.Name] {
				panic("duplicate struct field " + s.Name + "." + f.Name)
			}
			fields[f.Name] = true
		}
	}
	for _, f := range c.p.Funcs {
		checkPackage("func", f.Name)
		if len(f.Results) > 2 {
			panic("function " + f.Name + " has more than two results")
		}
		params := map[string]bool{}
		for _, p := range f.Params {
			checkDeclaredName("parameter", p.Name)
			if params[p.Name] {
				panic("duplicate parameter " + p.Name + " in " + f.Name)
			}
			params[p.Name] = true
		}
	}
}

func (c *checker) checkTypes() {
	for _, d := range c.p.Consts {
		c.checkType(d.Type, false)
	}
	for _, d := range c.p.Vars {
		c.checkType(d.Type, false)
	}
	for _, s := range c.p.Types {
		for _, f := range s.Fields {
			c.checkType(f.Type, false)
		}
	}
	for _, f := range c.p.Funcs {
		for _, p := range f.Params {
			c.checkType(p.Type, p.Type != nil && p.Type.K == KPtr)
		}
		for _, r := range f.Results {
			c.checkType(r, false)
		}
		WalkBody(f.Body, func(e *Expr) { c.checkType(e.TypeRef, false) }, func(s *Stmt) { c.checkType(s.TypeRef, false) })
	}
	state := map[string]uint8{}
	var visitStruct func(string)
	var visitType func(*Type)
	visitType = func(t *Type) {
		if t == nil {
			return
		}
		switch t.K {
		case KStruct:
			visitStruct(t.Name)
		case KArray:
			visitType(t.Elem)
		}
	}
	visitStruct = func(name string) {
		switch state[name] {
		case 1:
			panic("invalid recursive value type " + name)
		case 2:
			return
		}
		state[name] = 1
		for _, f := range c.p.StructMap[name].Fields {
			visitType(f.Type)
		}
		state[name] = 2
	}
	for _, s := range c.p.Types {
		visitStruct(s.Name)
	}
}

func (c *checker) checkType(t *Type, allowPointer bool) {
	if t == nil {
		return
	}
	switch t.K {
	case KSlice, KArray:
		c.checkType(t.Elem, false)
	case KStruct:
		if c.p.StructMap[t.Name] == nil {
			panic("unknown struct type " + t.Name)
		}
	case KPtr:
		if !allowPointer {
			panic("pointer types are limited to direct function parameters")
		}
		if c.p.StructMap[t.Name] == nil {
			panic("unknown struct type " + t.Name)
		}
	}
}

func checkNonBlankName(kind, name string) {
	if name == "_" {
		panic("the blank identifier cannot declare a " + kind)
	}
}

func checkDeclaredName(kind, name string) {
	checkNonBlankName(kind, name)
	checkIdentifier(kind, name)
	if name == "true" || name == "false" || name == "nil" {
		panic(kind + " cannot use predeclared name " + name)
	}
}

func checkIdentifier(kind, name string) {
	if name == "" || !asciiLetter(name[0]) && name[0] != '_' {
		panic(kind + " has invalid identifier " + name)
	}
	for i := 1; i < len(name); i++ {
		if !asciiLetter(name[i]) && (name[i] < '0' || name[i] > '9') && name[i] != '_' {
			panic(kind + " has invalid identifier " + name)
		}
	}
	if goKeyword(name) {
		panic(kind + " uses keyword " + name)
	}
}

func asciiLetter(b byte) bool {
	return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func goKeyword(name string) bool {
	switch name {
	case "break", "default", "func", "interface", "select", "case", "defer", "go", "map", "struct",
		"chan", "else", "goto", "package", "switch", "const", "fallthrough", "if", "range", "type",
		"continue", "for", "import", "return", "var":
		return true
	}
	return false
}

func predeclaredTypeName(name string) bool {
	switch name {
	case "bool", "uint8", "uint16", "uint32", "uint64", "int32", "int64", "int", "string":
		return true
	}
	return false
}

func (c *checker) packageName(name string) bool {
	if ReservedPackageName(name) {
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

// ReservedPackageName reports whether a package declaration would collide with generated runtime names.
func ReservedPackageName(name string) bool {
	return reserved[name] || strings.HasPrefix(name, tupleNamePrefix)
}

// ensureConst checks a constant declaration on demand.
// Constants may therefore reference each other in any source order.
func (c *checker) ensureConst(d *ValueDecl) {
	if d.Inferred != nil {
		return
	}
	if c.constState[d] == 1 {
		panic("constant initialization cycle at " + d.Name)
	}
	c.constState[d] = 1
	c.inferDecl(d)
	if d.Inferred == nil || d.Inferred.K != KBool && d.Inferred.K != KStr && !d.Inferred.IsInteger() || !d.Value.IsConst {
		panic("const " + d.Name + " does not have a scalar constant value")
	}
	if d.Inferred.IsInteger() {
		d.ConstVal = c.fold(d.Value)
	}
	c.constState[d] = 2
}

// inferDecl types the value of a declaration against its declared type.
// Without a declared type, it freezes the default type of the value.
func (c *checker) inferDecl(d *ValueDecl) {
	c.checkExpr(d.Value)
	if d.Value.Typ == nil || d.Value.Typ.K == KTuple {
		panic("declaration initializer must produce one value")
	}
	if d.Type != nil {
		c.retype(d.Value, d.Type)
		d.Inferred = d.Type
	} else {
		c.defaultType(d.Value)
		d.Inferred = d.Value.Typ
	}
}

// resolveArrayLens folds the length of every array type against the checked constants.
// Same can then compare lengths by value, and the printers can zero-pad partial array literals.
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
				v, ok := c.tryFold(t.ALen)
				if !ok {
					panic("array length is not a constant integer")
				}
				if v < 0 {
					panic("array length is negative")
				}
				t.ALenVal, t.ALenSet = v, true
				// The printers copy a literal or a named constant as written, so a computed length is replaced by its value.
				if t.ALen.K != "int" && t.ALen.K != "ident" {
					t.ALen = &Expr{K: "int", Value: strconv.FormatInt(v, 10)}
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

// tryFold folds an integer constant expression.
// It reports a failure instead of a panic.
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
	// The export package applies the same rules at the Go front end, with source positions.
	// This check guards the other JSON producers.
	if c.typeContainsSlice(d.Inferred) {
		panic("package variable " + d.Name + " contains a slice; globals are static constant data")
	}
	if !staticInitializer(d.Value) {
		panic("package variable " + d.Name + " has a non-constant initializer")
	}
}

func staticInitializer(e *Expr) bool {
	if e.IsConst {
		return true
	}
	if e.K != "composite" {
		return false
	}
	for _, f := range e.Fields {
		if !staticInitializer(f.Value) {
			return false
		}
	}
	for _, elem := range e.Elems {
		if !staticInitializer(elem) {
			return false
		}
	}
	return true
}

func (c *checker) typeContainsSlice(t *Type) bool {
	return c.typeContainsSliceSeen(t, map[string]bool{})
}

func (c *checker) typeContainsSliceSeen(t *Type, seen map[string]bool) bool {
	switch t.K {
	case KSlice:
		return true
	case KArray:
		return c.typeContainsSliceSeen(t.Elem, seen)
	case KStruct:
		if seen[t.Name] {
			return false
		}
		seen[t.Name] = true
		for _, f := range c.p.StructMap[t.Name].Fields {
			if c.typeContainsSliceSeen(f.Type, seen) {
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
	c.breakable = nil
	c.loopDepth = 0
	for i := range f.Params {
		pa := &f.Params[i]
		l := c.declare(pa.Name, pa.Type)
		pa.Name = l.unique
	}
	for _, s := range f.Body {
		c.checkStmt(s)
	}
	if len(f.Results) != 0 && !bodyTerminates(f.Body) {
		panic("function " + f.Name + " can reach its end without returning")
	}
	// Constant folding can remove reads and borrows.
	for _, info := range f.Info {
		info.Used = false
		info.Mutated = false
	}
	WalkBody(f.Body, func(e *Expr) {
		if e.K == "ident" {
			if info := f.Info[e.Name]; info != nil {
				info.Used = true
			}
		}
		if (e.K == "unary" && e.Op == "&") || (e.K == "slice_expr" && e.X.Typ.K == KArray) {
			c.markMutated(e.X)
		}
	}, func(s *Stmt) {
		if s.K == "assign" || s.K == "op_assign" {
			for _, lhs := range s.Lhs {
				c.markMutated(lhs)
			}
		}
	})
	c.scope = nil
	c.fn = nil
}

// declare adds a local.
// It renames the local when this function already took the name.
// It also renames it when the name clashes with a package-level or reserved name.
func (c *checker) declare(name string, t *Type) *local {
	unique := name
	if name != "_" {
		checkDeclaredName("local variable", name)
		if c.scope.vars[name] != nil {
			panic("local variable " + name + " is redeclared in the same scope")
		}
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
		checkNonBlankName("local variable", s.Name)
		if s.TypeRef == nil && s.Value == nil {
			panic("variable declaration needs a type or value")
		}
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
		if len(s.Names) != 1 && len(s.Names) != 2 {
			panic("short declaration must have one or two names")
		}
		if len(s.Names) == 1 {
			checkNonBlankName("local variable", s.Names[0])
		} else if s.Names[0] == "_" && s.Names[1] == "_" {
			panic("two-name define must declare a variable")
		}
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
		if len(s.Lhs) != 1 && len(s.Lhs) != 2 {
			panic("assignment must have one or two targets")
		}
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
				// Every printer evaluates the call before the places, but Go evaluates the places first.
				// A place with side effects therefore cannot translate.
				if Impure(l) {
					panic("two-value assign place with side effects is not supported")
				}
				c.markMutated(l)
			}
			checkAssignOrder(s.Lhs)
			return
		}
		l := s.Lhs[0]
		if l.K == "ident" && l.Name == "_" {
			c.checkExpr(s.Value)
			if s.Value.Typ == nil || s.Value.Typ.K == KTuple {
				panic("blank assignment requires one value")
			}
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
		if !lt.IsInteger() {
			panic("compound assignment requires integer operands")
		}
		c.markMutated(s.Lhs[0])
		c.checkExpr(s.Value)
		switch s.Op {
		case ">>=", "<<=":
			if s.Value.Untyped {
				c.retype(s.Value, TInt)
			}
			if !s.Value.Typ.IsInteger() {
				panic("shift count must be an integer")
			}
			if s.Value.IsConst && c.fold(s.Value).Sign() < 0 {
				panic("negative shift count")
			}
		case "+=", "-=", "*=", "/=", "%=", "|=", "&=", "^=", "&^=":
			c.retype(s.Value, lt)
			if (s.Op == "/=" || s.Op == "%=") && s.Value.IsConst && c.fold(s.Value).Sign() == 0 {
				panic("division by zero")
			}
		default:
			panic("unknown compound assignment operator " + s.Op)
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
			if s.Init.K != "define" || len(s.Init.Names) != 1 {
				panic("loop init must declare one variable from one expression")
			}
			c.checkStmt(s.Init)
		}
		if s.Cond != nil {
			c.checkExpr(s.Cond)
			c.retype(s.Cond, TBool)
		}
		// The body runs before the post statement.
		// The body scope must not capture the names of the post statement.
		c.breakable = append(c.breakable, true)
		c.loopDepth++
		c.checkBody(s.Body)
		c.loopDepth--
		c.breakable = c.breakable[:len(c.breakable)-1]
		if s.Post != nil {
			checkLoopPost(s.Post)
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
		case KInt:
			if s.ValName != "" {
				panic("range over an integer cannot have a value variable")
			}
		default:
			panic("range over unsupported type " + t.String())
		}
		if t.K == KArray && s.ValName == "" && exprContains(s.Value, "index") {
			panic("range over an indexed array with at most one variable is not supported")
		}
		// The lowered loops copy a hidden counter into the user variables on each iteration.
		// The mutability of those variables therefore follows the body alone.
		if s.IdxName != "" && s.IdxName != "_" {
			l := c.declare(s.IdxName, TInt)
			s.IdxName = l.unique
		}
		if s.ValName != "" && s.ValName != "_" {
			if elem == nil {
				panic("range value over int count")
			}
			if c.typeContainsSlice(elem) {
				panic("range value copies an element containing a slice")
			}
			l := c.declare(s.ValName, elem)
			s.ValName = l.unique
		}
		c.breakable = append(c.breakable, true)
		c.loopDepth++
		c.checkBody(s.Body)
		c.loopDepth--
		c.breakable = c.breakable[:len(c.breakable)-1]
		c.pop()
	case "switch":
		c.checkExpr(s.Tag)
		c.defaultType(s.Tag)
		tt := s.Tag.Typ
		if tt.K != KBool && !tt.IsInteger() {
			panic("switch tag must have bool or integer type")
		}
		c.breakable = append(c.breakable, false)
		caseValues := map[string]bool{}
		for _, cs := range s.Cases {
			for _, v := range cs.Values {
				c.checkExpr(v)
				c.retype(v, tt)
				if !v.IsConst {
					panic("switch case is not constant")
				}
				if key, ok := c.switchCaseKey(v); ok {
					if caseValues[key] {
						panic("duplicate switch case " + key)
					}
					caseValues[key] = true
				}
			}
			c.checkBody(cs.Body)
		}
		if s.HasDef {
			c.checkBody(s.Default)
		}
		c.breakable = c.breakable[:len(c.breakable)-1]
	case "break":
		if len(c.breakable) == 0 || !c.breakable[len(c.breakable)-1] {
			panic("break must target a loop")
		}
	case "continue":
		if c.loopDepth == 0 {
			panic("continue must be inside a loop")
		}
	case "return":
		if len(s.Values) == 1 && len(c.fn.Results) == 2 {
			// return f(...) forwarding both results.
			t := c.checkExpr(s.Values[0])
			if t == nil || t.K != KTuple || !Same(t.Tup[0], c.fn.Results[0]) || !Same(t.Tup[1], c.fn.Results[1]) {
				panic("bad two-result forward")
			}
			return
		}
		if len(s.Values) != len(c.fn.Results) {
			panic(fmt.Sprintf("return has %d values; function wants %d", len(s.Values), len(c.fn.Results)))
		}
		for i, v := range s.Values {
			c.checkExpr(v)
			c.retype(v, c.fn.Results[i])
		}
	case "expr_stmt":
		if s.Value.K != "call" && s.Value.K != "builtin" {
			panic("expression statements must be calls")
		}
		if s.Value.K == "builtin" && s.Value.Name != "copy" {
			panic("only copy may be used as a builtin expression statement")
		}
		c.checkExpr(s.Value)
	case "block":
		c.checkBody(s.Body)
	default:
		panic("unknown statement kind " + s.K)
	}
}

func (c *checker) switchCaseKey(e *Expr) (string, bool) {
	if n, ok := FoldConst(c.p, e); ok {
		return "integer " + n.String(), true
	}
	return "", false
}

func (c *checker) foldBool(e *Expr) (value bool, ok bool) {
	defer func() {
		if recover() != nil {
			value, ok = false, false
		}
	}()
	switch e.K {
	case "bool":
		return e.BoolVal, true
	case "ident":
		d := c.p.ConstMap[e.Name]
		if d != nil {
			return c.foldBool(d.Value)
		}
	case "conv":
		if e.TypeRef.K == KBool {
			return c.foldBool(e.X)
		}
	case "unary":
		if e.Op == "!" {
			v, valid := c.foldBool(e.X)
			return !v, valid
		}
	case "binary":
		switch e.Op {
		case "&&", "||":
			x, xOK := c.foldBool(e.X)
			y, yOK := c.foldBool(e.Y)
			if !xOK || !yOK {
				return false, false
			}
			if e.Op == "&&" {
				return x && y, true
			}
			return x || y, true
		case "==", "!=", "<", "<=", ">", ">=":
			cmp, valid := c.compareConstants(e.X, e.Y)
			if !valid {
				return false, false
			}
			switch e.Op {
			case "==":
				return cmp == 0, true
			case "!=":
				return cmp != 0, true
			case "<":
				return cmp < 0, true
			case "<=":
				return cmp <= 0, true
			case ">":
				return cmp > 0, true
			case ">=":
				return cmp >= 0, true
			}
		}
	}
	return false, false
}

func (c *checker) compareConstants(x, y *Expr) (int, bool) {
	if x.Typ.IsInteger() {
		return c.fold(x).Cmp(c.fold(y)), true
	}
	if x.Typ.K == KStr {
		return strings.Compare(c.foldString(x), c.foldString(y)), true
	}
	if x.Typ.K == KBool {
		xv, xOK := c.foldBool(x)
		yv, yOK := c.foldBool(y)
		if !xOK || !yOK {
			return 0, false
		}
		switch {
		case xv == yv:
			return 0, true
		case !xv && yv:
			return -1, true
		default:
			return 1, true
		}
	}
	return 0, false
}

func bodyTerminates(body []*Stmt) bool {
	return len(body) != 0 && stmtTerminates(body[len(body)-1])
}

func stmtTerminates(s *Stmt) bool {
	switch s.K {
	case "return":
		return true
	case "block":
		return bodyTerminates(s.Body)
	case "if":
		return s.HasElse && bodyTerminates(s.Then) && bodyTerminates(s.Else)
	case "for":
		return s.Cond == nil && !bodyBreaksLoop(s.Body)
	case "switch":
		if !s.HasDef || !bodyTerminates(s.Default) {
			return false
		}
		for _, cs := range s.Cases {
			if !bodyTerminates(cs.Body) {
				return false
			}
		}
		return true
	}
	return false
}

func bodyBreaksLoop(body []*Stmt) bool {
	for _, s := range body {
		switch s.K {
		case "break":
			return true
		case "for", "range":
			continue
		case "if":
			if bodyBreaksLoop(s.Then) || bodyBreaksLoop(s.Else) {
				return true
			}
		case "switch":
			for _, cs := range s.Cases {
				if bodyBreaksLoop(cs.Body) {
					return true
				}
			}
			if bodyBreaksLoop(s.Default) {
				return true
			}
		case "block":
			if bodyBreaksLoop(s.Body) {
				return true
			}
		}
	}
	return false
}

func checkLoopPost(s *Stmt) {
	switch s.K {
	case "assign":
		if len(s.Lhs) != 1 {
			panic("loop post must assign one value")
		}
		if s.Lhs[0].K == "ident" && s.Lhs[0].Name == "_" {
			panic("the blank identifier cannot be a loop post target")
		}
	case "op_assign":
		if len(s.Lhs) != 1 {
			panic("loop post must assign one value")
		}
		if s.Lhs[0].K == "ident" && s.Lhs[0].Name == "_" {
			panic("the blank identifier cannot be a loop post target")
		}
	case "expr_stmt":
	default:
		panic("loop post statement is not in the subset")
	}
}

// checkAssignOrder rejects a two-value assignment whose second target reads what the first one writes, as in i, x[i] = f().
// Go evaluates both targets before assigning, but the translations assign one after the other.
func checkAssignOrder(lhs []*Expr) {
	written := valuePath(lhs[0])
	if written == nil || written[0] == "_" {
		return
	}
	o := &orderWalk{}
	o.place(lhs[1])
	for _, ev := range o.events {
		if ev.read != nil && conflicts(ev.read, ev.readType, written) {
			panic("two-value assign place reads the earlier target " + written[0] + ", which the printers cannot order")
		}
	}
}

// checkPlace types an assignable expression.
func (c *checker) checkPlace(e *Expr) *Type {
	t := c.checkExpr(e)
	if c.globalPlace(e) {
		panic("assignment to package variable")
	}
	if !c.assignable(e) {
		panic("expression is not assignable")
	}
	return t
}

func (c *checker) globalPlace(e *Expr) bool {
	for e.K == "field" || e.K == "index" {
		e = e.X
	}
	_, ok := c.p.VarMap[e.Name]
	return e.K == "ident" && ok
}

func (c *checker) assignable(e *Expr) bool {
	switch e.K {
	case "ident":
		return c.fn.Info[e.Name] != nil
	case "field":
		return e.X.Typ.K == KPtr || c.assignable(e.X)
	case "index":
		return e.X.Typ.K == KSlice || e.X.Typ.K == KArray && c.assignable(e.X)
	}
	return false
}

func exprContains(e *Expr, kind string) bool {
	found := false
	WalkExpr(e, func(x *Expr) {
		if x.K == kind || kind == "slice_index" && x.K == "index" && x.X.Typ != nil && x.X.Typ.K == KSlice {
			found = true
		}
	})
	return found
}

// markMutated records that an assignment writes into the storage of a local.
// A write through a slice element or a pointer parameter lands outside the storage of the local.
// Such a write does not count.
// The identifier names already carry their unique form.
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

// checkExpr computes e.Typ from the bottom up.
// For a constant expression it sets Untyped and stores the default type.
// A parent then retypes the expression against its context, or keeps the default.
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
			// A constant declared without a type is still typed when its value is, as in const w = uint16(3).
			e.Untyped = d.Type == nil && !c.constTyped(d.Value)
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
		c.checkConstantIndex(e.X, e.Index)
	case "slice_expr":
		xt := c.checkExpr(e.X)
		if c.globalPlace(e.X) {
			panic("package-level data cannot be sliced")
		}
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
			if !c.assignable(e.X) {
				panic("cannot slice an unaddressable array")
			}
			e.Typ = &Type{K: KSlice, Elem: xt.Elem}
			// Slicing an array takes the address of its storage.
			c.markMutated(e.X)
		case KStr:
			e.Typ = TStr
		default:
			panic("slice of " + xt.String())
		}
		c.checkConstantSlice(e.X, e.Lo, e.Hi)
	case "call":
		f, ok := c.p.FuncMap[e.Name]
		if !ok {
			panic("unknown function " + e.Name)
		}
		if len(e.Args) != len(f.Params) {
			panic("bad argument count for " + e.Name)
		}
		for i, a := range e.Args {
			prior := c.allowAddr
			c.allowAddr = a.K == "unary" && a.Op == "&"
			c.checkExpr(a)
			c.allowAddr = prior
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
		from := c.checkExpr(e.X)
		to := e.TypeRef
		valid := from.IsInteger() && to.IsInteger() ||
			from.K == KBool && to.K == KBool ||
			from.K == KSlice && from.Elem.K == KU8 && to.K == KStr ||
			from.K == KStr && to.K == KSlice && to.Elem.K == KU8
		if !valid {
			panic("conversion outside the subset: " + from.String() + " to " + to.String())
		}
		if to.IsInteger() && e.X.IsConst {
			value := c.fold(e.X)
			if !integerFits(value, to) {
				panic(fmt.Sprintf("integer constant %s does not fit %s", value, to))
			}
		}
		if e.X.Untyped && (to.IsInteger() || to.K == KBool) {
			// This is a constant conversion, so the operand takes the target type directly.
			// Go guarantees that the value fits.
			c.retype(e.X, e.TypeRef)
		} else if e.X.Untyped {
			c.defaultType(e.X)
		}
		e.Typ = e.TypeRef
	case "unary":
		switch e.Op {
		case "-", "^":
			c.checkExpr(e.X)
			if !e.X.Typ.IsInteger() {
				panic("integer unary operator on " + e.X.Typ.String())
			}
			e.Typ = e.X.Typ
			e.Untyped = e.X.Untyped
		case "!":
			c.checkExpr(e.X)
			c.retype(e.X, TBool)
			e.Typ = TBool
		case "&":
			if !c.allowAddr {
				panic("address expressions are limited to direct call arguments")
			}
			xt := c.checkExpr(e.X)
			if xt.K != KStruct {
				panic("& on non-struct " + xt.String())
			}
			if e.X.K != "ident" && e.X.K != "field" || c.globalPlace(e.X) || !c.assignable(e.X) || exprContains(e.X, "slice_index") {
				panic("address operand is not a permitted struct variable or field")
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
			fields := map[string]bool{}
			for _, f := range e.Fields {
				if fields[f.Name] {
					panic("duplicate composite field " + f.Name)
				}
				fields[f.Name] = true
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
			if e.TypeRef.K == KArray && int64(len(e.Elems)) > e.TypeRef.ALenVal {
				panic("too many values in array composite")
			}
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
	if e.IsConst && !e.Untyped && e.Typ != nil && e.Typ.IsInteger() {
		value := c.fold(e)
		if !integerFits(value, e.Typ) {
			panic(fmt.Sprintf("integer constant %s does not fit %s", value, e.Typ))
		}
	}
	return e.Typ
}

// markConst flags compile-time constant expressions.
// The printers use the flag to decide how to emit a literal.
// A Zig comptime context and a C++ literal suffix both need that choice.
func (c *checker) markConst(e *Expr) {
	switch e.K {
	case "int", "char", "str", "bool":
		e.IsConst = true
	case "ident":
		// A local never shares a package-level name, because declare renames it.
		// checkExpr already rewrote this identifier.
		_, e.IsConst = c.p.ConstMap[e.Name]
	case "unary":
		e.IsConst = e.Op != "&" && e.X.IsConst
	case "binary":
		e.IsConst = e.X.IsConst && e.Y.IsConst
	case "conv":
		e.IsConst = (e.TypeRef.IsInteger() || e.TypeRef.K == KBool) && e.X.IsConst
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

func (c *checker) checkConstantIndex(container, index *Expr) {
	i, ok := FoldConst(c.p, index)
	if !ok {
		return
	}
	if i.Sign() < 0 {
		panic("constant index is negative")
	}
	if n := c.constantLength(container); n != nil && i.Cmp(n) >= 0 {
		panic("constant index is out of bounds")
	}
}

func (c *checker) checkConstantSlice(container, lo, hi *Expr) {
	var low, high *big.Int
	if lo == nil {
		low = new(big.Int)
	} else if v, ok := FoldConst(c.p, lo); ok {
		low = v
		if low.Sign() < 0 {
			panic("constant slice bound is negative")
		}
	}
	if hi != nil {
		if v, ok := FoldConst(c.p, hi); ok {
			high = v
			if high.Sign() < 0 {
				panic("constant slice bound is negative")
			}
		}
	} else {
		high = c.constantLength(container)
	}
	if low != nil && high != nil && low.Cmp(high) > 0 {
		panic("constant slice bounds are reversed")
	}
	if n := c.constantLength(container); n != nil {
		if low != nil && low.Cmp(n) > 0 || high != nil && high.Cmp(n) > 0 {
			panic("constant slice bound is out of range")
		}
	}
}

func (c *checker) constantLength(e *Expr) *big.Int {
	switch e.Typ.K {
	case KArray:
		if e.Typ.ALenSet {
			return big.NewInt(e.Typ.ALenVal)
		}
	case KStr:
		if e.IsConst {
			return big.NewInt(int64(len(c.foldString(e))))
		}
	}
	return nil
}

func (c *checker) checkBuiltin(e *Expr) {
	if e.Spread && e.Name != "append" {
		panic("spread arguments only belong to append")
	}
	switch e.Name {
	case "len":
		if len(e.Args) != 1 {
			panic("len takes exactly one argument")
		}
		c.checkExpr(e.Args[0])
		if e.Args[0].Typ.K != KStr && e.Args[0].Typ.K != KSlice && e.Args[0].Typ.K != KArray {
			panic("len applies only to strings, slices, and arrays")
		}
		e.Typ = TInt
		a := e.Args[0]
		var length *big.Int
		if a.Typ.K == KArray && !nonConstantCall(a) {
			length = c.fold(a.Typ.ALen)
		} else if a.Typ.K == KStr && a.IsConst {
			length = big.NewInt(int64(len(c.foldString(a))))
		}
		if length != nil {
			*e = Expr{K: "int", Value: length.String(), Typ: TInt, IsConst: true}
		}
	case "cap":
		if len(e.Args) != 1 {
			panic("cap takes exactly one argument")
		}
		c.checkExpr(e.Args[0])
		if e.Args[0].Typ.K != KSlice {
			panic("cap applies only to slices")
		}
		e.Typ = TInt
	case "make":
		if e.TypeRef == nil || e.TypeRef.K != KSlice || len(e.Args) < 1 || len(e.Args) > 2 {
			panic("make takes a slice type and one or two sizes")
		}
		for _, a := range e.Args {
			c.checkIndexOperand(a)
			if n, ok := FoldConst(c.p, a); ok && n.Sign() < 0 {
				panic("make size is negative")
			}
		}
		if len(e.Args) == 2 {
			length, lenOK := FoldConst(c.p, e.Args[0])
			capacity, capOK := FoldConst(c.p, e.Args[1])
			if lenOK && capOK && length.Cmp(capacity) > 0 {
				panic("make length exceeds capacity")
			}
		}
		e.Typ = e.TypeRef
	case "append":
		if len(e.Args) < 2 || e.Spread && len(e.Args) != 2 {
			panic("append needs at least one element")
		}
		st := c.checkExpr(e.Args[0])
		if st.K != KSlice {
			panic("append to " + st.String())
		}
		if e.Spread {
			a := e.Args[1]
			c.checkExpr(a)
			if a.Typ.K == KStr {
				if st.Elem.K != KU8 {
					panic("a string can spread only into []uint8")
				}
				c.retype(a, TStr)
			} else {
				c.retype(a, st)
			}
		} else {
			for _, a := range e.Args[1:] {
				c.checkExpr(a)
				c.retype(a, st.Elem)
			}
		}
		e.Typ = st
	case "copy":
		if len(e.Args) != 2 {
			panic("copy takes exactly two arguments")
		}
		dt := c.checkExpr(e.Args[0])
		c.checkExpr(e.Args[1])
		if dt.K != KSlice {
			panic("copy into " + dt.String())
		}
		st := e.Args[1].Typ
		if st.K == KStr {
			if dt.Elem.K != KU8 {
				panic("a string can copy only into []uint8")
			}
			c.retype(e.Args[1], TStr)
		} else {
			c.retype(e.Args[1], dt)
		}
		e.Typ = TInt
	case "min", "max":
		if len(e.Args) != 2 {
			panic(e.Name + " takes exactly two arguments")
		}
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
		if !x.Typ.IsInteger() {
			panic("min/max applies only to integers")
		}
		e.Typ = x.Typ
	default:
		panic("unknown builtin " + e.Name)
	}
}

func (c *checker) foldString(e *Expr) string {
	switch e.K {
	case "str":
		return e.Value
	case "ident":
		return c.foldString(c.p.ConstMap[e.Name].Value)
	case "builtin":
		a, b := c.foldString(e.Args[0]), c.foldString(e.Args[1])
		if e.Name == "min" {
			return min(a, b)
		}
		if e.Name == "max" {
			return max(a, b)
		}
	}
	panic("cannot fold string expression " + e.K)
}

// nonConstantCall decides whether an array-length operand must be evaluated.
func nonConstantCall(e *Expr) bool {
	found := false
	WalkExpr(e, func(x *Expr) {
		if (x.K == "call" || x.K == "builtin") && !x.IsConst {
			found = true
		}
	})
	return found
}

// unifyDefaults picks the default type of a constant operator pair.
// Rune wins over int, like Go.
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
		xNil, yNil := e.X.Typ.K == KNil, e.Y.Typ.K == KNil
		if xNil || yNil {
			if e.Op != "==" && e.Op != "!=" || xNil == yNil {
				panic("nil can only be compared with a slice")
			}
			if xNil {
				c.retype(e.X, e.Y.Typ)
			} else {
				c.retype(e.Y, e.X.Typ)
			}
			if e.X.Typ.K != KSlice || e.Y.Typ.K != KSlice {
				panic("nil can only be compared with a slice")
			}
		} else {
			c.unifyOperands(e)
		}
		if e.X.Typ.K == KPtr {
			panic("pointer comparisons are not in the subset")
		}
		switch e.X.Typ.K {
		case KBool, KSlice, KArray, KStruct:
			if e.Op != "==" && e.Op != "!=" {
				panic("ordered comparison of " + e.X.Typ.String())
			}
		case KStr:
		default:
			if !e.X.Typ.IsInteger() {
				panic("comparison of " + e.X.Typ.String())
			}
		}
		if (e.X.Typ.K == KArray || e.X.Typ.K == KStruct) && !c.comparable(e.X.Typ, map[string]bool{}) {
			panic("comparison of a value containing a slice")
		}
		if e.X.Typ.K == KSlice && !xNil && !yNil {
			panic("slices can only be compared with nil")
		}
		e.Untyped = e.X.Untyped && e.Y.Untyped
		// Operands never take a type from above a comparison.
		// A constant operand therefore freezes at its unified default here.
		c.defaultType(e.X)
		c.defaultType(e.Y)
		e.Typ = TBool
	case "<<", ">>":
		if e.X.Typ.K == KStr {
			panic("string operators are limited to comparisons")
		}
		if !e.X.Typ.IsInteger() {
			panic("shift of non-integer value")
		}
		if e.Y.Untyped {
			c.retype(e.Y, TInt)
		}
		if !e.Y.Typ.IsInteger() {
			panic("non-integer shift count")
		}
		if e.Y.IsConst && c.fold(e.Y).Sign() < 0 {
			panic("negative shift count")
		}
		e.Typ = e.X.Typ
		e.Untyped = e.X.Untyped
	case "+", "-", "*", "/", "%", "&", "|", "^", "&^":
		c.unifyOperands(e)
		if e.X.Typ.K == KStr {
			panic("string operators are limited to comparisons")
		}
		if !e.X.Typ.IsInteger() {
			panic("arithmetic operator requires integer operands")
		}
		if (e.Op == "/" || e.Op == "%") && e.Y.IsConst && c.fold(e.Y).Sign() == 0 {
			panic("division by zero")
		}
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
		if !sameConstantKind(x.Typ, y.Typ) {
			panic(fmt.Sprintf("operand type mismatch: %s %s %s", x.Typ, e.Op, y.Typ))
		}
		t := unifyDefaults(x.Typ, y.Typ)
		x.Typ = t
		y.Typ = t
	default:
		if !Same(x.Typ, y.Typ) {
			panic(fmt.Sprintf("operand type mismatch: %s %s %s", x.Typ, e.Op, y.Typ))
		}
	}
}

func sameConstantKind(a, b *Type) bool {
	return a.IsInteger() && b.IsInteger() || a.K == b.K && (a.K == KBool || a.K == KStr)
}

func (c *checker) comparable(t *Type, seen map[string]bool) bool {
	switch t.K {
	case KSlice:
		return false
	case KArray:
		return c.comparable(t.Elem, seen)
	case KStruct:
		if seen[t.Name] {
			return true
		}
		seen[t.Name] = true
		for _, f := range c.p.StructMap[t.Name].Fields {
			if !c.comparable(f.Type, seen) {
				return false
			}
		}
	}
	return true
}

// retype forces a concrete type onto an untyped constant expression, and it recurses into the structure of the constant.
// On an expression that already has a type, it only cross-checks.
func (c *checker) retype(e *Expr, t *Type) {
	c.retypeConstant(e, t, true)
}

func (c *checker) retypeConstant(e *Expr, t *Type, checkFit bool) {
	if t == nil {
		panic("value used where no value is expected")
	}
	if t.K == KTuple {
		panic("tuple in value position")
	}
	if !e.Untyped {
		if e.Typ == nil {
			panic("expression does not produce a value")
		}
		if !Same(e.Typ, t) {
			panic(fmt.Sprintf("type mismatch: have %s, want %s (%s)", e.Typ, t, e.K))
		}
		return
	}
	if e.Typ == nil || !(e.Typ.IsInteger() && t.IsInteger() || e.Typ.K == t.K && (t.K == KBool || t.K == KStr) || e.Typ.K == KNil && t.K == KSlice) {
		panic("untyped constant does not fit context " + t.String())
	}
	if checkFit && t.IsInteger() && e.IsConst {
		v := c.fold(e)
		if !integerFits(v, t) {
			panic(fmt.Sprintf("integer constant %s does not fit %s", v, t))
		}
	}
	e.Typ = t
	e.Untyped = false
	switch e.K {
	case "int", "char", "str", "bool", "ident":
	case "unary":
		c.retypeConstant(e.X, t, false)
	case "binary":
		switch e.Op {
		case "<<", ">>":
			c.retypeConstant(e.X, t, false)
		case "&&", "||", "==", "!=", "<", "<=", ">", ">=":
			// A comparison result is a plain bool, and the operands keep their own unified type.
			e.Typ = TBool
		default:
			c.retypeConstant(e.X, t, false)
			c.retypeConstant(e.Y, t, false)
		}
	case "builtin":
		if e.Name == "min" || e.Name == "max" {
			c.retypeConstant(e.Args[0], t, false)
			c.retypeConstant(e.Args[1], t, false)
		}
	default:
		panic("cannot retype " + e.K)
	}
}

// defaultType freezes an untyped constant at its default type.
func (c *checker) defaultType(e *Expr) {
	if e.Typ == nil || e.Typ.K == KTuple {
		panic("expression must produce one value")
	}
	if !e.Untyped {
		return
	}
	t := e.Typ
	if t.K == KI32 {
		// A rune-defaulted constant stays int32.
		t = TI32
	}
	// A constant shifted by a variable count is untyped without being constant, so only a constant folds.
	if t.IsInteger() && e.IsConst {
		v := c.fold(e)
		if !integerFits(v, t) {
			panic(fmt.Sprintf("integer constant %s does not fit %s", v, t))
		}
	}
	c.retype(e, t)
}

func integerFits(v *big.Int, t *Type) bool {
	w := uint(t.Width())
	if !t.Signed() {
		return v.Sign() >= 0 && v.BitLen() <= int(w)
	}
	limit := new(big.Int).Lsh(big.NewInt(1), w-1)
	min := new(big.Int).Neg(new(big.Int).Set(limit))
	max := new(big.Int).Sub(limit, big.NewInt(1))
	return v.Cmp(min) >= 0 && v.Cmp(max) <= 0
}

// fold evaluates an integer constant expression.
// It follows Go constant arithmetic: arbitrary precision first, then the declared type bounds the value.
// The source compiled, so no truncation happens.
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
			r := x.Not(x)
			// The complement of a typed unsigned constant stays inside its type, as in ^uint32(0).
			if c.constTyped(e.X) && e.X.Typ != nil && e.X.Typ.IsInteger() && !e.X.Typ.Signed() {
				r = truncateTo(r, e.X.Typ)
			}
			return r
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

// constTyped reports whether a constant expression is typed in Go.
// The checker gives untyped constants the type of their context, so the Untyped flag can't tell anymore.
// A conversion is typed, a named constant is typed when its declaration is, and an operator is typed when any of its operands is, except that a shift only looks at its left operand.
func (c *checker) constTyped(e *Expr) bool {
	switch e.K {
	case "conv":
		return true
	case "ident":
		d, ok := c.p.ConstMap[e.Name]
		if !ok {
			return false
		}
		if !d.typedKnown {
			d.typed = d.Type != nil || c.constTyped(d.Value)
			d.typedKnown = true
		}
		return d.typed
	case "unary":
		return c.constTyped(e.X)
	case "binary":
		if e.Op == "<<" || e.Op == ">>" {
			return c.constTyped(e.X)
		}
		return c.constTyped(e.X) || c.constTyped(e.Y)
	case "builtin":
		for _, a := range e.Args {
			if c.constTyped(a) {
				return true
			}
		}
	}
	return false
}

// ConstBelow reports whether e is a constant integer in the range [0, n).
func ConstBelow(p *Program, e *Expr, n int) bool {
	v, ok := FoldConst(p, e)
	return ok && v.Sign() >= 0 && v.Cmp(big.NewInt(int64(n))) < 0
}

// FoldConst evaluates a constant integer expression at the precision Go uses and returns the value its type holds.
// It reports false when e is not a constant integer expression.
func FoldConst(p *Program, e *Expr) (v *big.Int, ok bool) {
	if e == nil || !e.IsConst || e.Typ == nil || !e.Typ.IsInteger() {
		return nil, false
	}
	defer func() {
		if recover() != nil {
			v, ok = nil, false
		}
	}()
	c := &checker{p: p}
	return truncateTo(c.fold(e), e.Typ), true
}

// ConstExpr builds the literal form of an integer value of type t.
// A negative value becomes the negation of its magnitude, and the result carries no constant flag.
func ConstExpr(v *big.Int, t *Type) *Expr {
	if v.Sign() < 0 {
		magnitude := new(big.Int).Neg(v)
		return &Expr{K: "unary", Op: "-", Typ: t, X: &Expr{K: "int", Value: magnitude.String(), Typ: t}}
	}
	return &Expr{K: "int", Value: v.String(), Typ: t}
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

// rewriteMutatedParams gives a local shadow copy to every parameter the body assigns.
// A target can then keep its parameters read-only.
func (c *checker) rewriteMutatedParams(f *FuncDecl) {
	var pre []*Stmt
	for i := range f.Params {
		pa := &f.Params[i]
		info := f.Info[pa.Name]
		if info == nil || !info.Mutated {
			continue
		}
		shadow := pa.Name + "_v"
		for f.Info[shadow] != nil || c.packageName(shadow) {
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
