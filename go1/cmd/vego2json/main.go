// Command vego2json checks that a package conforms to the Vego subset.
// It then translates the package into the JSON representation that VEGO-SPECIFICATION.md defines.
//
// Usage:
//
//	vego2json [-o output.json] <package-directory>
//
// Files whose names end in _host.go or _test.go are host files, and the tool skips them.
// The tool reports any construct outside the subset with a file and a line.
// It then exits nonzero without output.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var scalarTypes = map[string]bool{
	"bool":   true,
	"uint8":  true,
	"uint16": true,
	"uint32": true,
	"uint64": true,
	"int32":  true,
	"int64":  true,
	"int":    true,
	"string": true,
}

var allowedBuiltins = map[string]bool{
	"len":    true,
	"cap":    true,
	"append": true,
	"make":   true,
	"copy":   true,
	"min":    true,
	"max":    true,
}

var compoundOps = map[token.Token]string{
	token.ADD_ASSIGN: "+=", token.SUB_ASSIGN: "-=",
	token.MUL_ASSIGN: "*=", token.QUO_ASSIGN: "/=",
	token.REM_ASSIGN: "%=", token.OR_ASSIGN: "|=",
	token.AND_ASSIGN: "&=", token.XOR_ASSIGN: "^=",
	token.SHL_ASSIGN: "<<=", token.SHR_ASSIGN: ">>=",
	token.AND_NOT_ASSIGN: "&^=",
}

var unaryOps = map[token.Token]string{
	token.SUB: "-", token.XOR: "^", token.NOT: "!",
}

var binaryOps = map[token.Token]string{
	token.ADD: "+", token.SUB: "-", token.MUL: "*",
	token.QUO: "/", token.REM: "%", token.SHL: "<<",
	token.SHR: ">>", token.AND: "&", token.OR: "|",
	token.XOR: "^", token.AND_NOT: "&^", token.LAND: "&&",
	token.LOR: "||", token.EQL: "==", token.NEQ: "!=",
	token.LSS: "<", token.LEQ: "<=", token.GTR: ">",
	token.GEQ: ">=",
}

type checker struct {
	fset    *token.FileSet
	info    *types.Info
	pkg     *types.Package
	structs map[string]bool
	errs    []string
	// breakable tracks the enclosing break targets, innermost last.
	// True marks a loop, and false marks a switch.
	breakable []bool
}

func (c *checker) errorf(pos token.Pos, format string, args ...any) {
	where := c.fset.Position(pos)
	c.errs = append(c.errs, fmt.Sprintf("%s: %s", where, fmt.Sprintf(format, args...)))
}

func isHostFile(name string) bool {
	return strings.HasSuffix(name, "_host.go") || strings.HasSuffix(name, "_test.go")
}

func main() {
	out := flag.String("o", "", "output file (default stdout)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: vego2json [-o output.json] <package-directory>")
		os.Exit(2)
	}
	blob, violations, err := translate(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(violations) > 0 {
		for _, e := range violations {
			fmt.Fprintln(os.Stderr, e)
		}
		fmt.Fprintf(os.Stderr, "%d subset violation(s)\n", len(violations))
		os.Exit(1)
	}
	if *out == "" {
		os.Stdout.Write(blob)
		return
	}
	if werr := os.WriteFile(*out, blob, 0o644); werr != nil {
		fmt.Fprintln(os.Stderr, werr)
		os.Exit(1)
	}
}

// translate checks the package in dir.
// It returns the JSON form of the package, or the list of subset violations.
func translate(dir string) ([]byte, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || isHostFile(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, nil, fmt.Errorf("no subset files found in %s", dir)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, name := range names {
		f, perr := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil, nil, perr
		}
		files = append(files, f)
	}

	// Imports leave the hermetic subset.
	// They would also defeat the importer-less type check below, so they stop the run first.
	var importErrs []string
	for _, f := range files {
		for _, imp := range f.Imports {
			where := fset.Position(imp.Pos())
			importErrs = append(importErrs,
				fmt.Sprintf("%s: imports are not in the subset", where))
		}
	}
	if len(importErrs) > 0 {
		return nil, importErrs, nil
	}

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Uses:  make(map[*ast.Ident]types.Object),
		Defs:  make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{DisableUnusedImportCheck: true}
	pkg, terr := conf.Check(files[0].Name.Name, fset, files, info)
	if terr != nil {
		return nil, nil, fmt.Errorf("type check: %w", terr)
	}

	c := &checker{fset: fset, info: info, pkg: pkg, structs: map[string]bool{}}
	for _, f := range files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, s := range gd.Specs {
					ts := s.(*ast.TypeSpec)
					if _, isStruct := ts.Type.(*ast.StructType); isStruct {
						c.structs[ts.Name.Name] = true
					}
				}
			}
		}
	}

	doc := map[string]any{
		"vego":    1,
		"package": files[0].Name.Name,
	}
	var consts, vars, typeDecls, funcs []any
	for _, f := range files {
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.GenDecl:
				switch decl.Tok {
				case token.CONST:
					consts = append(consts, c.constDecls(decl)...)
				case token.VAR:
					vars = append(vars, c.varDecls(decl)...)
				case token.TYPE:
					typeDecls = append(typeDecls, c.typeDecls(decl)...)
				case token.IMPORT:
					// Reported above.
				}
			case *ast.FuncDecl:
				funcs = append(funcs, c.funcDecl(decl))
			}
		}
	}
	doc["consts"] = orEmpty(consts)
	doc["vars"] = orEmpty(vars)
	doc["types"] = orEmpty(typeDecls)
	doc["funcs"] = orEmpty(funcs)

	if len(c.errs) > 0 {
		return nil, c.errs, nil
	}

	blob, jerr := json.MarshalIndent(doc, "", " ")
	if jerr != nil {
		return nil, nil, jerr
	}
	blob = append(blob, '\n')
	return blob, nil, nil
}

func orEmpty(list []any) []any {
	if list == nil {
		return []any{}
	}
	return list
}

// typeRef translates a type expression outside parameter position, where pointers are not allowed.
func (c *checker) typeRef(e ast.Expr) map[string]any {
	if _, isPtr := e.(*ast.StarExpr); isPtr {
		c.errorf(e.Pos(), "pointer types only appear as parameters")
	}
	return c.typeRefParam(e)
}

// typeRefParam translates a type expression in parameter position.
func (c *checker) typeRefParam(e ast.Expr) map[string]any {
	switch t := e.(type) {
	case *ast.Ident:
		if scalarTypes[t.Name] {
			return map[string]any{"k": "named", "name": t.Name}
		}
		if c.structs[t.Name] {
			return map[string]any{"k": "struct_ref", "name": t.Name}
		}
		c.errorf(t.Pos(), "type %s is not in the subset", t.Name)
		return map[string]any{"k": "named", "name": t.Name}
	case *ast.ArrayType:
		if t.Len == nil {
			return map[string]any{"k": "slice", "elem": c.typeRef(t.Elt)}
		}
		return map[string]any{"k": "array", "len": c.expr(t.Len),
			"elem": c.typeRef(t.Elt)}
	case *ast.StarExpr:
		name, ok := t.X.(*ast.Ident)
		if !ok || !c.structs[name.Name] {
			c.errorf(t.Pos(), "pointer type must name a declared struct")
			return map[string]any{"k": "ptr", "name": "?"}
		}
		return map[string]any{"k": "ptr", "name": name.Name}
	}
	c.errorf(e.Pos(), "type expression outside the subset")
	return map[string]any{"k": "named", "name": "?"}
}

func (c *checker) constDecls(decl *ast.GenDecl) []any {
	var out []any
	for _, s := range decl.Specs {
		vs := s.(*ast.ValueSpec)
		if len(vs.Values) != len(vs.Names) {
			c.errorf(vs.Pos(), "every constant needs an explicit value")
			continue
		}
		for i, name := range vs.Names {
			c.forbidIota(vs.Values[i])
			entry := map[string]any{"k": "const", "name": name.Name,
				"value": c.expr(vs.Values[i])}
			if vs.Type != nil {
				entry["type"] = c.typeRef(vs.Type)
			} else {
				entry["type"] = nil
			}
			out = append(out, entry)
		}
	}
	return out
}

func (c *checker) forbidIota(e ast.Expr) {
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == "iota" {
			c.errorf(id.Pos(), "iota is not in the subset")
		}
		return true
	})
}

// varDecls handles package-level immutable data.
func (c *checker) varDecls(decl *ast.GenDecl) []any {
	var out []any
	for _, s := range decl.Specs {
		vs := s.(*ast.ValueSpec)
		if len(vs.Values) != len(vs.Names) {
			c.errorf(vs.Pos(), "every package variable needs an initializer")
			continue
		}
		for i, name := range vs.Names {
			c.checkConstInitializer(vs.Values[i])
			if obj := c.info.Defs[name]; obj != nil {
				if typeContainsSlice(obj.Type()) {
					c.errorf(name.Pos(), "package variable types must not contain slices")
				}
			}
			entry := map[string]any{"k": "var", "name": name.Name,
				"value": c.expr(vs.Values[i])}
			if vs.Type != nil {
				entry["type"] = c.typeRef(vs.Type)
			} else {
				entry["type"] = nil
			}
			out = append(out, entry)
		}
	}
	return out
}

// typeContainsSlice walks a type for a slice at any depth.
// A package variable is static constant data in every target, and a slice buffer cannot be that.
// The vegoc checker applies the same rule over the JSON, for producers that bypass this tool.
func typeContainsSlice(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return true
	case *types.Array:
		return typeContainsSlice(u.Elem())
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if typeContainsSlice(u.Field(i).Type()) {
				return true
			}
		}
	}
	return false
}

// checkConstInitializer accepts constant expressions and composite literals of constants.
func (c *checker) checkConstInitializer(e ast.Expr) {
	if tv, ok := c.info.Types[e]; ok && tv.Value != nil {
		return
	}
	if lit, ok := e.(*ast.CompositeLit); ok {
		for _, el := range lit.Elts {
			c.checkConstInitializer(el)
		}
		return
	}
	c.errorf(e.Pos(), "package variable initializer must be constant data")
}

func (c *checker) typeDecls(decl *ast.GenDecl) []any {
	var out []any
	for _, s := range decl.Specs {
		ts := s.(*ast.TypeSpec)
		if ts.Assign.IsValid() {
			c.errorf(ts.Pos(), "type aliases are not in the subset")
			continue
		}
		if ts.TypeParams != nil {
			c.errorf(ts.Pos(), "generic types are not in the subset")
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			c.errorf(ts.Pos(), "only struct type declarations are in the subset")
			continue
		}
		var fields []any
		for _, f := range st.Fields.List {
			if len(f.Names) == 0 {
				c.errorf(f.Pos(), "embedded fields are not in the subset")
				continue
			}
			if _, isPtr := f.Type.(*ast.StarExpr); isPtr {
				c.errorf(f.Pos(), "pointer fields are not in the subset")
				continue
			}
			for _, name := range f.Names {
				fields = append(fields, map[string]any{
					"name": name.Name, "type": c.typeRef(f.Type)})
			}
		}
		out = append(out, map[string]any{"k": "type", "name": ts.Name.Name,
			"fields": orEmpty(fields)})
	}
	return out
}

func (c *checker) funcDecl(decl *ast.FuncDecl) any {
	if decl.Recv != nil {
		c.errorf(decl.Pos(), "methods are not in the subset")
	}
	if decl.Type.TypeParams != nil {
		c.errorf(decl.Pos(), "generic functions are not in the subset")
	}
	var params []any
	for _, f := range decl.Type.Params.List {
		if len(f.Names) == 0 {
			c.errorf(f.Pos(), "parameters must be named")
		}
		if _, variadic := f.Type.(*ast.Ellipsis); variadic {
			c.errorf(f.Pos(), "variadic parameters are not in the subset")
			continue
		}
		for _, name := range f.Names {
			params = append(params, map[string]any{
				"name": name.Name, "type": c.typeRefParam(f.Type)})
		}
	}
	var results []any
	if decl.Type.Results != nil {
		for _, f := range decl.Type.Results.List {
			if len(f.Names) > 0 {
				c.errorf(f.Pos(), "named results are not in the subset")
			}
			n := max(len(f.Names), 1)
			for i := 0; i < n; i++ {
				results = append(results, c.typeRef(f.Type))
			}
		}
		if len(results) > 2 {
			c.errorf(decl.Pos(), "more than two results")
		}
	}
	c.breakable = c.breakable[:0]
	body := c.block(decl.Body)
	return map[string]any{"k": "func", "name": decl.Name.Name,
		"params": orEmpty(params), "results": orEmpty(results),
		"body": body}
}

func (c *checker) block(b *ast.BlockStmt) []any {
	var out []any
	for _, s := range b.List {
		out = append(out, c.stmt(s))
	}
	return orEmpty(out)
}

func (c *checker) stmt(s ast.Stmt) any {
	switch st := s.(type) {
	case *ast.DeclStmt:
		gd, ok := st.Decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR || len(gd.Specs) != 1 {
			c.errorf(st.Pos(), "only a single var declaration is allowed here")
			return map[string]any{"k": "block", "body": []any{}}
		}
		vs := gd.Specs[0].(*ast.ValueSpec)
		if len(vs.Names) != 1 {
			c.errorf(st.Pos(), "one variable per declaration")
		}
		entry := map[string]any{"k": "var_decl", "name": vs.Names[0].Name}
		if vs.Type != nil {
			entry["type"] = c.typeRef(vs.Type)
		} else {
			entry["type"] = nil
		}
		if len(vs.Values) == 1 {
			entry["value"] = c.expr(vs.Values[0])
		} else {
			entry["value"] = nil
		}
		return entry
	case *ast.AssignStmt:
		return c.assign(st)
	case *ast.IncDecStmt:
		op := "++"
		if st.Tok == token.DEC {
			op = "--"
		}
		c.checkAssignable(st.X)
		return map[string]any{"k": "incdec", "op": op, "lhs": c.expr(st.X)}
	case *ast.IfStmt:
		if st.Init != nil {
			c.errorf(st.Pos(), "if with an init statement is not in the subset")
		}
		entry := map[string]any{"k": "if", "cond": c.expr(st.Cond),
			"then": c.block(st.Body)}
		switch e := st.Else.(type) {
		case nil:
			entry["else"] = nil
		case *ast.BlockStmt:
			entry["else"] = c.block(e)
		case *ast.IfStmt:
			entry["else"] = []any{c.stmt(e)}
		}
		return entry
	case *ast.ForStmt:
		c.breakable = append(c.breakable, true)
		entry := map[string]any{"k": "for"}
		if st.Init != nil {
			if _, ok := st.Init.(*ast.AssignStmt); !ok {
				c.errorf(st.Init.Pos(), "loop init must be a short declaration")
			}
			entry["init"] = c.stmt(st.Init)
		} else {
			entry["init"] = nil
		}
		if st.Cond != nil {
			entry["cond"] = c.expr(st.Cond)
		} else {
			entry["cond"] = nil
		}
		if st.Post != nil {
			entry["post"] = c.stmt(st.Post)
		} else {
			entry["post"] = nil
		}
		entry["body"] = c.block(st.Body)
		c.breakable = c.breakable[:len(c.breakable)-1]
		return entry
	case *ast.RangeStmt:
		return c.rangeStmt(st)
	case *ast.SwitchStmt:
		return c.switchStmt(st)
	case *ast.BranchStmt:
		if st.Label != nil {
			c.errorf(st.Pos(), "labels are not in the subset")
		}
		if st.Tok == token.BREAK {
			if len(c.breakable) == 0 ||
				!c.breakable[len(c.breakable)-1] {
				c.errorf(st.Pos(), "break must target a loop")
			}
			return map[string]any{"k": "break"}
		}
		if st.Tok == token.CONTINUE {
			return map[string]any{"k": "continue"}
		}
		c.errorf(st.Pos(), "%s is not in the subset", st.Tok)
		return map[string]any{"k": "block", "body": []any{}}
	case *ast.ReturnStmt:
		var values []any
		for _, e := range st.Results {
			values = append(values, c.expr(e))
		}
		return map[string]any{"k": "return", "values": orEmpty(values)}
	case *ast.ExprStmt:
		if _, ok := st.X.(*ast.CallExpr); !ok {
			c.errorf(st.Pos(), "expression statements must be calls")
		}
		return map[string]any{"k": "expr_stmt", "value": c.expr(st.X)}
	case *ast.BlockStmt:
		return map[string]any{"k": "block", "body": c.block(st)}
	}
	c.errorf(s.Pos(), "statement outside the subset: %T", s)
	return map[string]any{"k": "block", "body": []any{}}
}

func (c *checker) assign(st *ast.AssignStmt) any {
	if st.Tok == token.DEFINE {
		var names []any
		for _, l := range st.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok {
				c.errorf(l.Pos(), "short declaration must bind identifiers")
				continue
			}
			names = append(names, id.Name)
		}
		if len(st.Rhs) != 1 {
			c.errorf(st.Pos(), "one expression on the right of :=")
		}
		if len(st.Lhs) > 2 {
			c.errorf(st.Pos(), "more than two names in a short declaration")
		}
		if len(st.Lhs) == 2 {
			if _, ok := st.Rhs[0].(*ast.CallExpr); !ok {
				c.errorf(st.Pos(), "a two-name declaration needs a call")
			}
		}
		return map[string]any{"k": "define", "names": names,
			"value": c.expr(st.Rhs[0])}
	}
	if st.Tok == token.ASSIGN {
		if len(st.Rhs) != 1 {
			c.errorf(st.Pos(), "one expression on the right of =")
		}
		if len(st.Lhs) > 2 {
			c.errorf(st.Pos(), "more than two assignment targets")
		}
		if len(st.Lhs) == 2 {
			if _, ok := st.Rhs[0].(*ast.CallExpr); !ok {
				c.errorf(st.Pos(), "a two-target assignment needs a call")
			}
		}
		var lhs []any
		for _, l := range st.Lhs {
			c.checkAssignable(l)
			if len(st.Lhs) == 1 {
				c.checkSliceStore(l, st.Rhs[0])
			}
			lhs = append(lhs, c.expr(l))
		}
		return map[string]any{"k": "assign", "lhs": lhs,
			"value": c.expr(st.Rhs[0])}
	}
	// Compound assignment.
	op, ok := compoundOps[st.Tok]
	if !ok || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		c.errorf(st.Pos(), "assignment form outside the subset")
		return map[string]any{"k": "block", "body": []any{}}
	}
	c.checkAssignable(st.Lhs[0])
	return map[string]any{"k": "op_assign", "op": op,
		"lhs": c.expr(st.Lhs[0]), "value": c.expr(st.Rhs[0])}
}

// checkSliceStore applies the locally decidable part of the buffer model.
// A slice-typed struct field or element takes only a fresh buffer, a moved variable or field, or a truncation of itself.
func (c *checker) checkSliceStore(lhs ast.Expr, rhs ast.Expr) {
	switch lhs.(type) {
	case *ast.SelectorExpr, *ast.IndexExpr:
	default:
		// Local variables may hold transient views.
		return
	}
	t := c.info.Types[lhs].Type
	if t == nil {
		return
	}
	if _, isSlice := t.Underlying().(*types.Slice); !isSlice {
		return
	}
	switch r := rhs.(type) {
	case *ast.CallExpr, *ast.CompositeLit, *ast.Ident, *ast.SelectorExpr:
		// Fresh buffers, call results, and moves.
		return
	case *ast.SliceExpr:
		if types.ExprString(r.X) == types.ExprString(lhs) {
			// Truncation of the same owner.
			return
		}
	}
	c.errorf(rhs.Pos(), "a slice field takes a fresh buffer, a move, or a truncation of itself")
}

// checkAssignable rejects writes to package-level variables.
func (c *checker) checkAssignable(e ast.Expr) {
	if c.globalBase(e) != nil {
		c.errorf(e.Pos(), "package-level data is immutable")
	}
}

func (c *checker) rangeStmt(st *ast.RangeStmt) any {
	if st.Tok == token.ASSIGN {
		c.errorf(st.Pos(), "range must declare its variables")
	}
	overType := c.info.Types[st.X].Type
	if overType != nil {
		switch under := overType.Underlying().(type) {
		case *types.Slice, *types.Array:
		case *types.Basic:
			if under.Info()&types.IsInteger == 0 {
				c.errorf(st.X.Pos(), "range is only over slices, arrays, and integer counts")
			}
		default:
			c.errorf(st.X.Pos(), "range is only over slices, arrays, and integer counts")
		}
	}
	entry := map[string]any{"k": "range", "over": c.expr(st.X)}
	entry["idx"] = nil
	entry["val"] = nil
	if st.Key != nil {
		id, isIdent := st.Key.(*ast.Ident)
		if !isIdent {
			c.errorf(st.Key.Pos(), "range must bind plain identifiers")
		} else {
			entry["idx"] = id.Name
		}
	}
	if st.Value != nil {
		id, isIdent := st.Value.(*ast.Ident)
		if !isIdent {
			c.errorf(st.Value.Pos(), "range must bind plain identifiers")
		} else {
			entry["val"] = id.Name
			if vt := c.info.Types[st.Value].Type; vt != nil && containsSlice(vt) {
				c.errorf(st.Value.Pos(), "range value copies of slice-holding elements are not in the subset")
			}
		}
	}
	c.breakable = append(c.breakable, true)
	entry["body"] = c.block(st.Body)
	c.breakable = c.breakable[:len(c.breakable)-1]
	return entry
}

func containsSlice(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return true
	case *types.Struct:
		for i := 0; i < u.NumFields(); i++ {
			if containsSlice(u.Field(i).Type()) {
				return true
			}
		}
	case *types.Array:
		return containsSlice(u.Elem())
	}
	return false
}

func (c *checker) switchStmt(st *ast.SwitchStmt) any {
	if st.Init != nil {
		c.errorf(st.Pos(), "switch with an init statement is not in the subset")
	}
	if st.Tag == nil {
		c.errorf(st.Pos(), "switch needs a scalar tag")
		return map[string]any{"k": "block", "body": []any{}}
	}
	entry := map[string]any{"k": "switch", "tag": c.expr(st.Tag)}
	var cases []any
	var defaultBody any
	c.breakable = append(c.breakable, false)
	for _, raw := range st.Body.List {
		cl := raw.(*ast.CaseClause)
		var body []any
		for _, s := range cl.Body {
			if br, isBranch := s.(*ast.BranchStmt); isBranch &&
				br.Tok == token.FALLTHROUGH {
				c.errorf(s.Pos(), "fallthrough is not in the subset")
				continue
			}
			body = append(body, c.stmt(s))
		}
		if cl.List == nil {
			defaultBody = orEmpty(body)
			continue
		}
		var values []any
		for _, v := range cl.List {
			if tv, ok := c.info.Types[v]; !ok || tv.Value == nil {
				c.errorf(v.Pos(), "case values must be constants")
			}
			values = append(values, c.expr(v))
		}
		cases = append(cases, map[string]any{"values": values,
			"body": orEmpty(body)})
	}
	c.breakable = c.breakable[:len(c.breakable)-1]
	entry["cases"] = orEmpty(cases)
	entry["default"] = defaultBody
	return entry
}

func (c *checker) expr(e ast.Expr) any {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return c.expr(x.X)
	case *ast.BasicLit:
		switch x.Kind {
		case token.INT:
			v, err := strconv.ParseUint(strings.ReplaceAll(x.Value, "_", ""), 0, 64)
			if err != nil {
				c.errorf(x.Pos(), "unsupported integer literal %s", x.Value)
			}
			return map[string]any{"k": "int", "value": strconv.FormatUint(v, 10)}
		case token.CHAR:
			r, _, _, err := strconv.UnquoteChar(x.Value[1:len(x.Value)-1], '\'')
			if err != nil {
				c.errorf(x.Pos(), "bad character literal %s", x.Value)
			}
			return map[string]any{"k": "char", "value": strconv.FormatInt(int64(r), 10)}
		case token.STRING:
			s, err := strconv.Unquote(x.Value)
			if err != nil {
				c.errorf(x.Pos(), "bad string literal")
			}
			return map[string]any{"k": "str", "value": s}
		}
		c.errorf(x.Pos(), "literal kind %s is not in the subset", x.Kind)
		return map[string]any{"k": "int", "value": "0"}
	case *ast.Ident:
		if x.Name == "true" || x.Name == "false" {
			return map[string]any{"k": "bool", "value": x.Name == "true"}
		}
		if x.Name == "iota" {
			c.errorf(x.Pos(), "iota is not in the subset")
		}
		return map[string]any{"k": "ident", "name": x.Name}
	case *ast.SelectorExpr:
		return map[string]any{"k": "field", "x": c.expr(x.X),
			"name": x.Sel.Name}
	case *ast.IndexExpr:
		return map[string]any{"k": "index", "x": c.expr(x.X),
			"index": c.expr(x.Index)}
	case *ast.SliceExpr:
		if x.Slice3 {
			c.errorf(x.Pos(), "three-index slices are not in the subset")
		}
		if c.globalBase(x.X) != nil {
			c.errorf(x.Pos(), "package-level data cannot be sliced")
		}
		entry := map[string]any{"k": "slice_expr", "x": c.expr(x.X)}
		if x.Low != nil {
			entry["lo"] = c.expr(x.Low)
		} else {
			entry["lo"] = nil
		}
		if x.High != nil {
			entry["hi"] = c.expr(x.High)
		} else {
			entry["hi"] = nil
		}
		return entry
	case *ast.CallExpr:
		return c.call(x)
	case *ast.UnaryExpr:
		op, ok := unaryOps[x.Op]
		if !ok {
			if x.Op == token.AND {
				c.errorf(x.Pos(), "& appears only as a call argument")
				op = "&"
			} else {
				c.errorf(x.Pos(), "unary %s is not in the subset", x.Op)
				op = "?"
			}
		}
		return map[string]any{"k": "unary", "op": op, "x": c.expr(x.X)}
	case *ast.BinaryExpr:
		op, ok := binaryOps[x.Op]
		if !ok {
			c.errorf(x.Pos(), "operator %s is not in the subset", x.Op)
			op = "?"
		}
		return map[string]any{"k": "binary", "op": op,
			"x": c.expr(x.X), "y": c.expr(x.Y)}
	case *ast.CompositeLit:
		return c.composite(x)
	}
	c.errorf(e.Pos(), "expression outside the subset: %T", e)
	return map[string]any{"k": "int", "value": "0"}
}

// exprArg translates one call argument, the only position that allows &.
func (c *checker) exprArg(a ast.Expr) any {
	if u, ok := a.(*ast.UnaryExpr); ok && u.Op == token.AND {
		c.checkAddress(u.X)
		return map[string]any{"k": "unary", "op": "&", "x": c.expr(u.X)}
	}
	return c.expr(a)
}

// baseExpr unwraps parentheses, indexes, fields and slices down to the base identifier, or to nil.
func baseExpr(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.SliceExpr:
			e = x.X
		default:
			id, _ := e.(*ast.Ident)
			return id
		}
	}
}

// globalBase returns the base identifier of e when it names a package-level variable, and nil in every other case.
// Every global rule goes through it: never written, never sliced, and never borrowed.
func (c *checker) globalBase(e ast.Expr) *ast.Ident {
	id := baseExpr(e)
	if id == nil || id.Name == "_" {
		return nil
	}
	obj, found := c.info.Uses[id]
	if !found {
		return nil
	}
	if _, isVar := obj.(*types.Var); !isVar {
		return nil
	}
	if obj.Parent() != c.pkg.Scope() {
		return nil
	}
	return id
}

// checkAddress restricts & to struct variables and struct fields.
func (c *checker) checkAddress(e ast.Expr) {
	t := c.info.Types[e].Type
	if t != nil {
		if _, isStruct := t.Underlying().(*types.Struct); !isStruct {
			c.errorf(e.Pos(), "& applies only to struct values")
		}
	}
	switch e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
	default:
		c.errorf(e.Pos(), "& applies only to struct variables and fields")
		return
	}
	if c.globalBase(e) != nil {
		c.errorf(e.Pos(), "& on package-level data is not in the subset")
	}
}

func (c *checker) call(x *ast.CallExpr) any {
	// A conversion parses as a call whose function is a type.
	if tv, ok := c.info.Types[x.Fun]; ok && tv.IsType() {
		if len(x.Args) != 1 {
			c.errorf(x.Pos(), "conversion needs one operand")
		}
		c.checkConversion(x)
		return map[string]any{"k": "conv", "type": c.typeRef(x.Fun),
			"x": c.expr(x.Args[0])}
	}
	name, ok := x.Fun.(*ast.Ident)
	if !ok {
		c.errorf(x.Pos(), "calls must name a package function or builtin")
		return map[string]any{"k": "call", "fn": "?", "args": []any{}}
	}
	obj := c.info.Uses[name]
	if b, isBuiltin := obj.(*types.Builtin); isBuiltin {
		if !allowedBuiltins[b.Name()] {
			c.errorf(x.Pos(), "builtin %s is not in the subset", b.Name())
		}
		return c.builtin(x, b.Name())
	}
	if x.Ellipsis.IsValid() {
		c.errorf(x.Pos(), "spread arguments only belong to append")
	}
	var args []any
	for _, a := range x.Args {
		args = append(args, c.exprArg(a))
	}
	return map[string]any{"k": "call", "fn": name.Name,
		"args": orEmpty(args)}
}

func (c *checker) checkConversion(x *ast.CallExpr) {
	target := c.info.Types[x.Fun].Type
	source := c.info.Types[x.Args[0]].Type
	if target == nil || source == nil {
		return
	}
	tb, tIsBasic := target.Underlying().(*types.Basic)
	sb, sIsBasic := source.Underlying().(*types.Basic)
	sSlice, sIsSlice := source.Underlying().(*types.Slice)
	tSlice, tIsSlice := target.Underlying().(*types.Slice)
	switch {
	case tIsBasic && sIsBasic &&
		tb.Info()&types.IsString == 0 && sb.Info()&types.IsString == 0:
		// Scalar to scalar.
	case tIsBasic && tb.Info()&types.IsString != 0 && sIsSlice &&
		isUint8(sSlice.Elem()):
		// []uint8 to string.
	case tIsSlice && isUint8(tSlice.Elem()) && sIsBasic &&
		sb.Info()&types.IsString != 0:
		// string to []uint8.
	default:
		c.errorf(x.Pos(), "conversion outside the subset")
	}
}

func isUint8(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

func (c *checker) builtin(x *ast.CallExpr, name string) any {
	entry := map[string]any{"k": "builtin", "fn": name,
		"spread": x.Ellipsis.IsValid()}
	if x.Ellipsis.IsValid() && name != "append" {
		c.errorf(x.Pos(), "spread arguments only belong to append")
	}
	var args []any
	for i, a := range x.Args {
		if name == "make" && i == 0 {
			entry["type"] = c.typeRef(a)
			continue
		}
		args = append(args, c.exprArg(a))
	}
	if name != "make" {
		entry["type"] = nil
	}
	entry["args"] = orEmpty(args)
	if name == "make" {
		if len(x.Args) < 2 || len(x.Args) > 3 {
			c.errorf(x.Pos(), "make takes a slice type and one or two sizes")
		}
	}
	if name == "append" && len(x.Args) < 2 {
		c.errorf(x.Pos(), "append needs at least one element")
	}
	if (name == "min" || name == "max") && len(x.Args) != 2 {
		c.errorf(x.Pos(), "%s takes exactly two arguments", name)
	}
	return entry
}

func (c *checker) composite(x *ast.CompositeLit) any {
	if x.Type == nil {
		c.errorf(x.Pos(), "composite literals must name their type")
		return map[string]any{"k": "int", "value": "0"}
	}
	entry := map[string]any{"k": "composite", "type": c.typeRef(x.Type)}
	isStruct := false
	if id, ok := x.Type.(*ast.Ident); ok && c.structs[id.Name] {
		isStruct = true
	}
	if isStruct {
		var fields []any
		for _, el := range x.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				c.errorf(el.Pos(), "struct literals need field keys")
				continue
			}
			fields = append(fields, map[string]any{
				"name":  kv.Key.(*ast.Ident).Name,
				"value": c.expr(kv.Value)})
		}
		entry["fields"] = orEmpty(fields)
		return entry
	}
	var elems []any
	for _, el := range x.Elts {
		if _, keyed := el.(*ast.KeyValueExpr); keyed {
			c.errorf(el.Pos(), "keyed elements are not in the subset")
			continue
		}
		elems = append(elems, c.expr(el))
	}
	entry["elems"] = orEmpty(elems)
	return entry
}
