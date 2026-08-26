package vegoc

// This file decides which functions need a memory context.
//
// Generated code has no global allocator. Every function that can
// allocate receives an explicit context as a synthetic first
// parameter, which the printers name "mem". The flag computed here
// tells each printer where that parameter goes.

// ExprAllocates reports whether the expression node itself
// allocates: a make, an append, a string conversion that copies,
// or a slice composite literal. Calls are not covered; whether a
// callee allocates is the transitive question below. This is the
// one definition of an allocation site; the printers emit "mem"
// for exactly these forms.
func ExprAllocates(e *Expr) bool {
	switch e.K {
	case "builtin":
		return e.Name == "make" || e.Name == "append"
	case "conv":
		return e.TypeRef.K == KStr || e.TypeRef.K == KSlice
	case "composite":
		return e.TypeRef.K == KSlice
	}
	return false
}

// directAllocates reports whether the body of f allocates by
// itself, calls aside.
func directAllocates(f *FuncDecl) bool {
	found := false
	WalkBody(f.Body, func(e *Expr) {
		if ExprAllocates(e) {
			found = true
		}
	}, nil)
	return found
}

// markAllocates fills FuncDecl.Allocates for the whole program. A
// function allocates when its body does, or when it calls a
// function that allocates. The loop runs to a fixed point, so
// recursion and any call order are fine.
func markAllocates(p *Program) {
	for _, f := range p.Funcs {
		f.Allocates = directAllocates(f)
	}
	for changed := true; changed; {
		changed = false
		for _, f := range p.Funcs {
			if f.Allocates {
				continue
			}
			WalkBody(f.Body, func(e *Expr) {
				if e.K == "call" && p.CalleeAllocates(e.Name) {
					f.Allocates = true
					changed = true
				}
			}, nil)
		}
	}
}

// CalleeAllocates reports whether a call to the named function
// takes the memory context. Printers use it to prepend "mem" at
// call sites, so the rule lives in one place.
func (p *Program) CalleeAllocates(name string) bool {
	f, ok := p.FuncMap[name]
	return ok && f.Allocates
}
