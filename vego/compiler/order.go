package compiler

// This file rejects statements whose result depends on an evaluation order that Go leaves unspecified.
//
// Go runs the calls of a statement from left to right, but doesn't say when it reads the other operands relative to those calls.
// gc reads them after the calls, while the printers keep the source order.
// So a statement must not read a variable before a call that can write to it.
//
// The check only sees reads that name the variable.
// A read through another view of the same buffer is up to the program author, like the move rules in section 6 of the specification.

import (
	"fmt"
	"strings"
)

// SharesStorage reports whether passing a value of type t lets the callee write to the caller's storage.
// That is the case for pointers and slices, and for structs and arrays that contain one.
func (p *Program) SharesStorage(t *Type) bool {
	switch t.K {
	case KPtr, KSlice:
		return true
	case KArray:
		return p.SharesStorage(t.Elem)
	case KStruct:
		if sd := p.StructMap[t.Name]; sd != nil {
			for _, f := range sd.Fields {
				if p.SharesStorage(f.Type) {
					return true
				}
			}
		}
	}
	return false
}

// storageRoot returns the variable at the root of a place or a view, or "" if there is none.
func storageRoot(e *Expr) string {
	for {
		switch e.K {
		case "field", "index", "slice_expr":
			e = e.X
		case "unary":
			if e.Op != "&" {
				return ""
			}
			e = e.X
		case "ident":
			return e.Name
		default:
			return ""
		}
	}
}

// writesThrough reports whether writing to e changes storage outside its root variable, which happens when e goes through a pointer or a slice.
func writesThrough(e *Expr) bool {
	for {
		switch e.K {
		case "field":
			if e.X.Typ != nil && e.X.Typ.K == KPtr {
				return true
			}
			e = e.X
		case "index":
			if e.X.Typ != nil && e.X.Typ.K == KSlice {
				return true
			}
			e = e.X
		default:
			return false
		}
	}
}

// paramWrites lists, for each function, the parameters through which a call can write to the caller's storage.
type paramWrites map[string]map[int]bool

// markParamWrites computes paramWrites for the whole program, repeating until nothing changes.
// A local that shares a parameter's storage, as a view or a copy, counts as that parameter.
func markParamWrites(p *Program) paramWrites {
	w := paramWrites{}
	aliases := map[*FuncDecl]map[string]map[int]bool{}
	for _, f := range p.Funcs {
		w[f.Name] = map[int]bool{}
		a := map[string]map[int]bool{}
		for i, pa := range f.Params {
			if p.SharesStorage(pa.Type) {
				a[pa.Name] = map[int]bool{i: true}
			}
		}
		aliases[f] = a
	}
	for changed := true; changed; {
		changed = false
		for _, f := range p.Funcs {
			a := aliases[f]
			mark := func(root string) {
				changed = addAll(w[f.Name], a[root]) || changed
			}
			bind := func(name string, t *Type, value *Expr) {
				if value == nil || t == nil || !p.SharesStorage(t) {
					return
				}
				// append can return the buffer of its first argument.
				for value.K == "builtin" && value.Name == "append" && len(value.Args) > 0 {
					value = value.Args[0]
				}
				if src := a[storageRoot(value)]; len(src) > 0 {
					if a[name] == nil {
						a[name] = map[int]bool{}
					}
					changed = addAll(a[name], src) || changed
				}
			}
			WalkBody(f.Body, func(e *Expr) {
				for _, pa := range w.writtenPaths(e) {
					mark(pa[0])
				}
			}, func(s *Stmt) {
				switch s.K {
				case "assign", "op_assign":
					for _, l := range s.Lhs {
						if writesThrough(l) {
							mark(storageRoot(l))
						}
					}
					if s.K == "assign" && len(s.Lhs) == 1 && s.Lhs[0].K == "ident" {
						bind(s.Lhs[0].Name, s.Lhs[0].Typ, s.Value)
					}
				case "define":
					if len(s.Names) == 1 && len(s.DeclaredTypes) == 1 {
						bind(s.Names[0], s.DeclaredTypes[0], s.Value)
					}
				case "var_decl":
					bind(s.Name, s.TypeRef, s.Value)
				}
			})
		}
	}
	return w
}

// A storagePath names what an expression reads or what a call can write, starting from a variable.
// Its steps are field names, "*" for a pointer, "[a]" for an array element, and "[s]" for an element of a slice's buffer.
// Indexes are ignored, so all the elements of an array or a buffer count as one.
type storagePath []string

// valuePath returns the storage path of a place, or nil if e isn't one.
func valuePath(e *Expr) storagePath {
	switch e.K {
	case "ident":
		return storagePath{e.Name}
	case "field":
		base := valuePath(e.X)
		if base == nil {
			return nil
		}
		if e.X.Typ != nil && e.X.Typ.K == KPtr {
			base = append(base, "*")
		}
		return append(base, e.Name)
	case "index":
		if e.X.Typ != nil && e.X.Typ.K == KSlice {
			return elemsPath(e.X)
		}
		if e.X.Typ != nil && e.X.Typ.K == KArray {
			if base := valuePath(e.X); base != nil {
				return append(base, "[a]")
			}
		}
	}
	return nil
}

// elemsPath returns the storage path of the elements a slice refers to, or nil if they can't be named.
func elemsPath(e *Expr) storagePath {
	if e.K == "slice_expr" {
		if e.X.Typ != nil && e.X.Typ.K == KArray {
			if base := valuePath(e.X); base != nil {
				return append(base, "[a]")
			}
			return nil
		}
		if e.X.Typ != nil && e.X.Typ.K == KSlice {
			return elemsPath(e.X)
		}
		return nil
	}
	if base := valuePath(e); base != nil {
		return append(base, "[s]")
	}
	return nil
}

// writtenPaths returns the storage a call can write through its arguments.
// Each path covers everything below it.
// copy writes to its destination, and append can write to the spare capacity of its first argument.
func (w paramWrites) writtenPaths(e *Expr) []storagePath {
	var paths []storagePath
	add := func(pa storagePath) {
		if pa != nil {
			paths = append(paths, pa)
		}
	}
	switch e.K {
	case "call":
		for j, a := range e.Args {
			if !w[e.Name][j] {
				continue
			}
			switch {
			case a.K == "unary" && a.Op == "&":
				add(valuePath(a.X))
			case a.Typ != nil && a.Typ.K == KPtr:
				if base := valuePath(a); base != nil {
					add(append(base, "*"))
				}
			case a.Typ != nil && a.Typ.K == KSlice:
				add(elemsPath(a))
			default:
				// A struct or an array holding pointers or slices, which the callee can write through.
				add(valuePath(a))
			}
		}
	case "builtin":
		if (e.Name == "copy" || e.Name == "append") && len(e.Args) > 0 {
			add(elemsPath(e.Args[0]))
		}
	}
	return paths
}

// addAll adds the elements of src to dst and reports whether dst grew.
func addAll(dst map[int]bool, src map[int]bool) bool {
	grew := false
	for i := range src {
		if !dst[i] {
			dst[i] = true
			grew = true
		}
	}
	return grew
}

// orderEvent is a read, or a call that can write, in the order it appears in a statement.
type orderEvent struct {
	read     storagePath
	readType *Type
	writes   []storagePath
	// sequenced holds the event ranges Go always runs before the call: its own arguments, and the left side of any && or || that has the call on its right.
	sequenced [][2]int
}

type orderWalk struct {
	w      paramWrites
	fn     string
	events []orderEvent
	// logical holds the event ranges of the left sides of the && and || operators whose right side is being walked.
	logical [][2]int
}

func (o *orderWalk) read(pa storagePath, t *Type) {
	if pa != nil {
		o.events = append(o.events, orderEvent{read: pa, readType: t})
	}
}

// value walks an expression that is evaluated for its value.
func (o *orderWalk) value(e *Expr) {
	if e == nil {
		return
	}
	switch e.K {
	case "ident", "field", "index":
		if pa := valuePath(e); pa != nil {
			o.place(e)
			o.read(pa, e.Typ)
			return
		}
	case "slice_expr":
		if e.X.Typ != nil && e.X.Typ.K == KArray {
			// Slicing an array reads none of its elements.
			o.place(e.X)
		} else {
			o.value(e.X)
		}
		o.value(e.Lo)
		o.value(e.Hi)
		return
	case "unary":
		if e.Op == "&" {
			// Taking an address reads nothing.
			o.place(e.X)
			return
		}
	case "call", "builtin":
		from := len(o.events)
		for _, a := range e.Args {
			o.value(a)
		}
		if writes := o.w.writtenPaths(e); len(writes) > 0 {
			sequenced := append([][2]int{{from, len(o.events)}}, o.logical...)
			o.events = append(o.events, orderEvent{writes: writes, sequenced: sequenced})
		}
		return
	case "binary":
		if e.Op == "&&" || e.Op == "||" {
			// Go evaluates the left operand of a logical operator before its right operand.
			from := len(o.events)
			o.value(e.X)
			o.logical = append(o.logical, [2]int{from, len(o.events)})
			o.value(e.Y)
			o.logical = o.logical[:len(o.logical)-1]
			return
		}
	}
	for _, sub := range []*Expr{e.X, e.Y, e.Index, e.Lo, e.Hi} {
		o.value(sub)
	}
	for _, el := range e.Elems {
		o.value(el)
	}
	for _, f := range e.Fields {
		o.value(f.Value)
	}
}

// place walks a place without reading the value stored there.
// It still reads its indexes, and every slice or pointer it goes through.
func (o *orderWalk) place(e *Expr) {
	switch e.K {
	case "field":
		o.place(e.X)
		if e.X.Typ != nil && e.X.Typ.K == KPtr {
			o.read(valuePath(e.X), e.X.Typ)
		}
	case "index":
		if e.X.Typ != nil && e.X.Typ.K == KSlice {
			o.value(e.X)
		} else {
			o.place(e.X)
		}
		o.value(e.Index)
	}
}

// conflicts reports whether reading r, of type t, can observe a write to w or to anything below it.
// That is the case when r is inside w, or when r reads a whole value that holds w directly, not behind a pointer or a slice.
func conflicts(r storagePath, t *Type, w storagePath) bool {
	n := min(len(r), len(w))
	for i := 0; i < n; i++ {
		if r[i] != w[i] {
			return false
		}
	}
	if len(r) >= len(w) {
		return true
	}
	if t == nil || t.K == KSlice || t.K == KPtr {
		return false
	}
	for _, step := range w[len(r):] {
		if step == "*" || step == "[s]" {
			return false
		}
	}
	return true
}

// check rejects a read that comes before a call that can write to it, unless Go always evaluates the read first.
func (o *orderWalk) check() {
	for t, call := range o.events {
		for _, w := range call.writes {
			for i, ev := range o.events[:t] {
				if ev.read == nil || !conflicts(ev.read, ev.readType, w) || sequencedBefore(i, call.sequenced) {
					continue
				}
				panic(fmt.Sprintf("%s: %s is read before a call that can write it, an order Go leaves unspecified",
					o.fn, strings.Join(ev.read, ".")))
			}
		}
	}
}

func sequencedBefore(i int, ranges [][2]int) bool {
	for _, r := range ranges {
		if i >= r[0] && i < r[1] {
			return true
		}
	}
	return false
}

// checkEvalOrder applies the rule to every statement in the program.
func checkEvalOrder(p *Program) {
	w := markParamWrites(p)
	for _, f := range p.Funcs {
		WalkBody(f.Body, nil, func(s *Stmt) {
			o := &orderWalk{w: w, fn: f.Name}
			switch s.K {
			case "assign":
				for _, l := range s.Lhs {
					if l.K == "ident" && l.Name == "_" {
						continue
					}
					o.place(l)
				}
				o.value(s.Value)
			case "op_assign":
				// x op= y reads x before y.
				for _, l := range s.Lhs {
					o.value(l)
				}
				o.value(s.Value)
			case "define", "var_decl", "expr_stmt", "range":
				o.value(s.Value)
			case "return":
				for _, v := range s.Values {
					o.value(v)
				}
			case "if", "for":
				o.value(s.Cond)
			case "switch":
				o.value(s.Tag)
			}
			o.check()
		})
	}
}
