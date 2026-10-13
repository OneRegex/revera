package compiler

import (
	"strings"
	"testing"
)

func checkedViewProgram(p *Program) (err error) {
	return Check(p)
}

func viewProgram(types []*StructDecl, funcs ...*FuncDecl) *Program {
	p := &Program{
		Package:   "t",
		Types:     types,
		Funcs:     funcs,
		ConstMap:  map[string]*ValueDecl{},
		VarMap:    map[string]*ValueDecl{},
		StructMap: map[string]*StructDecl{},
		FuncMap:   map[string]*FuncDecl{},
	}
	for _, typ := range types {
		p.StructMap[typ.Name] = typ
	}
	for _, f := range funcs {
		f.Info = map[string]*LocalInfo{}
		p.FuncMap[f.Name] = f
	}
	return p
}

func viewArray(elem *Type) *Type {
	return &Type{K: KArray, Elem: elem, ALen: &Expr{K: "int", Value: "1"}}
}

func viewSlice(elem *Type) *Type {
	return &Type{K: KSlice, Elem: elem}
}

func viewIdent(name string) *Expr {
	return &Expr{K: "ident", Name: name}
}

func viewSliceExpr(name string) *Expr {
	return &Expr{K: "slice_expr", X: viewIdent(name)}
}

func viewFunction(name string, params []Param, results []*Type, body ...*Stmt) *FuncDecl {
	return &FuncDecl{Name: name, Params: params, Results: results, Body: body}
}

func TestCheckViewsRejectsLocalArrayEscapes(t *testing.T) {
	cases := []struct {
		name string
		make func() *Program
		want string
	}{
		{
			name: "return",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "return", Values: []*Expr{viewSliceExpr("a")}},
				))
			},
			want: "must not be returned",
		},
		{
			name: "outer lexical scope",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "block", Body: []*Stmt{
						{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
						{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
					}},
				))
			},
			want: "must not outlive",
		},
		{
			name: "field",
			make: func() *Program {
				slice := viewSlice(TInt)
				holder := &StructDecl{Name: "Holder", Fields: []Param{{Name: "data", Type: slice}}}
				return viewProgram([]*StructDecl{holder}, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "var_decl", Name: "h", TypeRef: &Type{K: KStruct, Name: "Holder"}},
					&Stmt{K: "assign", Lhs: []*Expr{{K: "field", X: viewIdent("h"), Name: "data"}}, Value: viewSliceExpr("a")},
				))
			},
			want: "stored in a field or an element",
		},
		{
			name: "append element",
			make: func() *Program {
				elem := viewSlice(TInt)
				dst := viewSlice(elem)
				return viewProgram(nil, viewFunction("f", []Param{{Name: "dst", Type: dst}}, nil,
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("_")}, Value: &Expr{
						K: "builtin", Name: "append", Args: []*Expr{viewIdent("dst"), viewSliceExpr("a")},
					}},
				))
			},
			want: "stored in a field or an element",
		},
		{
			name: "call result",
			make: func() *Program {
				slice := viewSlice(TInt)
				identity := viewFunction("identity", []Param{{Name: "s", Type: slice}}, []*Type{slice},
					&Stmt{K: "return", Values: []*Expr{viewIdent("s")}},
				)
				caller := viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "return", Values: []*Expr{{K: "call", Name: "identity", Args: []*Expr{viewSliceExpr("a")}}}},
				)
				return viewProgram(nil, identity, caller)
			},
			want: "must not be returned",
		},
		{
			name: "loop carried alias",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "var_decl", Name: "v", TypeRef: slice},
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "for",
						Init: &Stmt{K: "define", Names: []string{"i"}, Value: &Expr{K: "int", Value: "0"}},
						Cond: &Expr{K: "binary", Op: "<", X: viewIdent("i"), Y: &Expr{K: "int", Value: "2"}},
						Post: &Stmt{K: "op_assign", Op: "+=", Lhs: []*Expr{viewIdent("i")}, Value: &Expr{K: "int", Value: "1"}},
						Body: []*Stmt{
							{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewIdent("v")},
							{K: "assign", Lhs: []*Expr{viewIdent("v")}, Value: viewSliceExpr("a")},
						},
					},
					&Stmt{K: "return", Values: []*Expr{viewIdent("out")}},
				))
			},
			want: "must not be returned",
		},
		{
			name: "pointer alias through call",
			make: func() *Program {
				array := viewArray(TInt)
				slice := viewSlice(TInt)
				record := &StructDecl{Name: "Record", Fields: []Param{{Name: "values", Type: array}}}
				ptr := &Type{K: KPtr, Name: "Record"}
				choose := viewFunction("choose", []Param{{Name: "p", Type: ptr}, {Name: "q", Type: ptr}}, []*Type{slice},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("p")}, Value: viewIdent("q")},
					&Stmt{K: "return", Values: []*Expr{{K: "slice_expr", X: &Expr{K: "field", X: viewIdent("p"), Name: "values"}}}},
				)
				caller := viewFunction("f", []Param{{Name: "external", Type: ptr}}, []*Type{slice},
					&Stmt{K: "var_decl", Name: "local", TypeRef: &Type{K: KStruct, Name: "Record"}},
					&Stmt{K: "return", Values: []*Expr{{K: "call", Name: "choose", Args: []*Expr{
						viewIdent("external"), {K: "unary", Op: "&", X: viewIdent("local")},
					}}}},
				)
				return viewProgram([]*StructDecl{record}, choose, caller)
			},
			want: "must not be returned",
		},
		{
			name: "parallel assignment reads old values",
			make: func() *Program {
				slice := viewSlice(TInt)
				pair := viewFunction("pair", []Param{{Name: "s", Type: slice}}, []*Type{slice, slice},
					&Stmt{K: "return", Values: []*Expr{
						{K: "builtin", Name: "make", TypeRef: slice, Args: []*Expr{{K: "int", Value: "1"}}},
						viewIdent("s"),
					}},
				)
				caller := viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "define", Names: []string{"x"}, Value: viewSliceExpr("a")},
					&Stmt{K: "var_decl", Name: "y", TypeRef: slice},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("x"), viewIdent("y")}, Value: &Expr{
						K: "call", Name: "pair", Args: []*Expr{viewIdent("x")},
					}},
					&Stmt{K: "return", Values: []*Expr{viewIdent("y")}},
				)
				return viewProgram(nil, pair, caller)
			},
			want: "must not be returned",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkedViewProgram(tc.make())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("checkViews error = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestCheckViewsAcceptsNonEscapingDataflow(t *testing.T) {
	cases := []struct {
		name string
		make func() *Program
	}{
		{
			name: "mutually exclusive branches",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", []Param{{Name: "take", Type: TBool}}, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "var_decl", Name: "v", TypeRef: slice},
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "if", Cond: viewIdent("take"), HasElse: true,
						Then: []*Stmt{{K: "assign", Lhs: []*Expr{viewIdent("v")}, Value: viewSliceExpr("a")}},
						Else: []*Stmt{{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewIdent("v")}},
					},
					&Stmt{K: "return", Values: []*Expr{viewIdent("out")}},
				))
			},
		},
		{
			name: "strong overwrite",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "var_decl", Name: "v", TypeRef: slice},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("v")}, Value: viewSliceExpr("a")},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("v")}, Value: &Expr{
						K: "builtin", Name: "make", TypeRef: slice, Args: []*Expr{{K: "int", Value: "1"}},
					}},
					&Stmt{K: "return", Values: []*Expr{viewIdent("v")}},
				))
			},
		},
		{
			name: "parameter and outer body share a lifetime",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", []Param{{Name: "dst", Type: slice}}, nil,
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "assign", Lhs: []*Expr{viewIdent("dst")}, Value: viewSliceExpr("a")},
				))
			},
		},
		{
			name: "constant false if",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "if", Cond: &Expr{K: "bool", BoolVal: false}, Then: []*Stmt{
						{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
						{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
					}},
				))
			},
		},
		{
			name: "constant switch",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "switch", Tag: &Expr{K: "int", Value: "0"}, Cases: []SwitchCase{
						{Values: []*Expr{{K: "int", Value: "0"}}},
						{Values: []*Expr{{K: "int", Value: "1"}}, Body: []*Stmt{
							{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
							{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
						}},
					}},
				))
			},
		},
		{
			name: "constant false loop",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "for", Cond: &Expr{K: "bool", BoolVal: false}, Body: []*Stmt{
						{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
						{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
					}},
				))
			},
		},
		{
			name: "zero integer range",
			make: func() *Program {
				slice := viewSlice(TInt)
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "range", IdxName: "i", Value: &Expr{K: "int", Value: "0"}, Body: []*Stmt{
						{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
						{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
					}},
				))
			},
		},
		{
			name: "zero array range",
			make: func() *Program {
				slice := viewSlice(TInt)
				empty := &Type{K: KArray, Elem: TInt, ALen: &Expr{K: "int", Value: "0"}}
				return viewProgram(nil, viewFunction("f", nil, nil,
					&Stmt{K: "var_decl", Name: "out", TypeRef: slice},
					&Stmt{K: "var_decl", Name: "empty", TypeRef: empty},
					&Stmt{K: "range", IdxName: "_", Value: viewIdent("empty"), Body: []*Stmt{
						{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
						{K: "assign", Lhs: []*Expr{viewIdent("out")}, Value: viewSliceExpr("a")},
					}},
				))
			},
		},
		{
			name: "separate call results",
			make: func() *Program {
				slice := viewSlice(TInt)
				pair := viewFunction("pair", []Param{{Name: "s", Type: slice}}, []*Type{slice, slice},
					&Stmt{K: "return", Values: []*Expr{
						{K: "builtin", Name: "make", TypeRef: slice, Args: []*Expr{{K: "int", Value: "1"}}},
						viewIdent("s"),
					}},
				)
				caller := viewFunction("f", nil, []*Type{slice},
					&Stmt{K: "var_decl", Name: "a", TypeRef: viewArray(TInt)},
					&Stmt{K: "define", Names: []string{"safe", "_"}, Value: &Expr{
						K: "call", Name: "pair", Args: []*Expr{viewSliceExpr("a")},
					}},
					&Stmt{K: "return", Values: []*Expr{viewIdent("safe")}},
				)
				return viewProgram(nil, pair, caller)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := checkedViewProgram(tc.make()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
