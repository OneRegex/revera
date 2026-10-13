package compiler

type viewOrigin struct {
	scope int
	param int
}

type viewSet map[viewOrigin]struct{}
type viewEnv map[string]viewSet
type viewParamSet map[int]bool
type viewSummaries map[string][]viewParamSet

type viewFlow struct {
	normal    viewEnv
	breaks    viewEnv
	continues viewEnv
}

type viewAnalysis struct {
	p          *Program
	f          *FuncDecl
	summaries  viewSummaries
	validate   bool
	parents    []int
	declScopes map[string]int
	returns    []viewParamSet
}

// checkViews verifies that views of local storage cannot escape their lexical lifetime.
func checkViews(p *Program) {
	summaries := make(viewSummaries, len(p.Funcs))
	for _, f := range p.Funcs {
		summaries[f.Name] = make([]viewParamSet, len(f.Results))
		for i := range f.Results {
			summaries[f.Name][i] = viewParamSet{}
		}
	}

	for grew := true; grew; {
		grew = false
		for _, f := range p.Funcs {
			a := newViewAnalysis(p, f, summaries, false)
			a.run()
			for i, params := range a.returns {
				for param := range params {
					if !summaries[f.Name][i][param] {
						summaries[f.Name][i][param] = true
						grew = true
					}
				}
			}
		}
	}

	for _, f := range p.Funcs {
		newViewAnalysis(p, f, summaries, true).run()
	}
}

func newViewAnalysis(p *Program, f *FuncDecl, summaries viewSummaries, validate bool) *viewAnalysis {
	a := &viewAnalysis{
		p:          p,
		f:          f,
		summaries:  summaries,
		validate:   validate,
		parents:    []int{-1},
		declScopes: map[string]int{},
		returns:    make([]viewParamSet, len(f.Results)),
	}
	for i := range a.returns {
		a.returns[i] = viewParamSet{}
	}
	for _, param := range f.Params {
		a.declScopes[param.Name] = 0
	}
	a.indexBody(f.Body, 0)
	return a
}

func (a *viewAnalysis) newScope(parent int) int {
	a.parents = append(a.parents, parent)
	return len(a.parents) - 1
}

func (a *viewAnalysis) indexBody(body []*Stmt, scope int) {
	for _, s := range body {
		a.indexStmt(s, scope)
	}
}

func (a *viewAnalysis) indexStmt(s *Stmt, scope int) {
	if s == nil {
		return
	}
	switch s.K {
	case "var_decl":
		a.declScopes[s.Name] = scope
	case "define":
		for _, name := range s.Names {
			if name != "_" {
				a.declScopes[name] = scope
			}
		}
	case "if":
		a.indexBody(s.Then, a.newScope(scope))
		if s.HasElse {
			a.indexBody(s.Else, a.newScope(scope))
		}
	case "for":
		loop := a.newScope(scope)
		a.indexStmt(s.Init, loop)
		a.indexBody(s.Body, a.newScope(loop))
		a.indexStmt(s.Post, loop)
	case "range":
		loop := a.newScope(scope)
		if s.IdxName != "" && s.IdxName != "_" {
			a.declScopes[s.IdxName] = loop
		}
		if s.ValName != "" && s.ValName != "_" {
			a.declScopes[s.ValName] = loop
		}
		a.indexBody(s.Body, a.newScope(loop))
	case "switch":
		for _, cs := range s.Cases {
			a.indexBody(cs.Body, a.newScope(scope))
		}
		if s.HasDef {
			a.indexBody(s.Default, a.newScope(scope))
		}
	case "block":
		a.indexBody(s.Body, a.newScope(scope))
	}
}

func (a *viewAnalysis) run() {
	env := viewEnv{}
	for i, param := range a.f.Params {
		if viewType(param.Type) {
			env[param.Name] = viewSet{{scope: -1, param: i}: {}}
		}
	}
	a.body(a.f.Body, env)
}

func viewType(t *Type) bool {
	return t != nil && (t.K == KSlice || t.K == KPtr)
}

func cloneViewSet(s viewSet) viewSet {
	out := make(viewSet, len(s))
	for origin := range s {
		out[origin] = struct{}{}
	}
	return out
}

func cloneViewEnv(env viewEnv) viewEnv {
	if env == nil {
		return nil
	}
	out := make(viewEnv, len(env))
	for name, origins := range env {
		out[name] = cloneViewSet(origins)
	}
	return out
}

func joinViewSets(a, b viewSet) viewSet {
	out := cloneViewSet(a)
	for origin := range b {
		out[origin] = struct{}{}
	}
	return out
}

func joinViewEnvs(a, b viewEnv) viewEnv {
	if a == nil {
		return cloneViewEnv(b)
	}
	if b == nil {
		return cloneViewEnv(a)
	}
	out := cloneViewEnv(a)
	for name, origins := range b {
		out[name] = joinViewSets(out[name], origins)
	}
	return out
}

func equalViewEnvs(a, b viewEnv) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	for name, origins := range a {
		other := b[name]
		if len(origins) != len(other) {
			return false
		}
		for origin := range origins {
			if _, ok := other[origin]; !ok {
				return false
			}
		}
	}
	for name, origins := range b {
		if len(origins) != len(a[name]) {
			return false
		}
	}
	return true
}

func (a *viewAnalysis) body(body []*Stmt, env viewEnv) viewFlow {
	flow := viewFlow{normal: cloneViewEnv(env)}
	for _, s := range body {
		if flow.normal == nil {
			break
		}
		next := a.stmt(s, flow.normal)
		flow.normal = next.normal
		flow.breaks = joinViewEnvs(flow.breaks, next.breaks)
		flow.continues = joinViewEnvs(flow.continues, next.continues)
	}
	return flow
}

func (a *viewAnalysis) stmt(s *Stmt, env viewEnv) viewFlow {
	if s == nil {
		return viewFlow{normal: env}
	}
	a.validateStmtExprs(s, env)
	switch s.K {
	case "var_decl":
		a.bind(env, s.Name, s.TypeRef, a.value(s.Value, 0, env))
	case "define":
		for i, name := range s.Names {
			var typ *Type
			if i < len(s.DeclaredTypes) {
				typ = s.DeclaredTypes[i]
			}
			a.bind(env, name, typ, a.value(s.Value, i, env))
		}
	case "assign":
		values := make([]viewSet, len(s.Lhs))
		for i := range s.Lhs {
			values[i] = a.value(s.Value, i, env)
		}
		for i, lhs := range s.Lhs {
			origins := values[i]
			if lhs.K == "ident" {
				a.bind(env, lhs.Name, lhs.Typ, origins)
			} else if a.validate && len(origins) != 0 {
				panic("a view must not be stored in a field or an element")
			}
		}
	case "if":
		if value, ok := a.constantBool(s.Cond); ok {
			if value {
				return a.body(s.Then, env)
			}
			if s.HasElse {
				return a.body(s.Else, env)
			}
			return viewFlow{normal: env}
		}
		thenFlow := a.body(s.Then, env)
		elseFlow := viewFlow{normal: cloneViewEnv(env)}
		if s.HasElse {
			elseFlow = a.body(s.Else, env)
		}
		return viewFlow{
			normal:    joinViewEnvs(thenFlow.normal, elseFlow.normal),
			breaks:    joinViewEnvs(thenFlow.breaks, elseFlow.breaks),
			continues: joinViewEnvs(thenFlow.continues, elseFlow.continues),
		}
	case "for":
		return a.forStmt(s, env)
	case "range":
		return a.rangeStmt(s, env)
	case "switch":
		return a.switchStmt(s, env)
	case "break":
		return viewFlow{breaks: cloneViewEnv(env)}
	case "continue":
		return viewFlow{continues: cloneViewEnv(env)}
	case "return":
		a.returnStmt(s, env)
		return viewFlow{}
	case "block":
		return a.body(s.Body, env)
	}
	return viewFlow{normal: env}
}

func (a *viewAnalysis) forStmt(s *Stmt, env viewEnv) viewFlow {
	entry := cloneViewEnv(env)
	if s.Init != nil {
		entry = a.stmt(s.Init, entry).normal
	}
	conditionExit := s.Cond != nil
	if value, ok := a.constantBool(s.Cond); ok {
		if !value {
			return viewFlow{normal: entry}
		}
		conditionExit = false
	}
	head := cloneViewEnv(entry)
	for {
		iteration := a.body(s.Body, head)
		back := joinViewEnvs(iteration.normal, iteration.continues)
		if back != nil && s.Post != nil {
			back = a.stmt(s.Post, back).normal
		}
		next := joinViewEnvs(entry, back)
		if equalViewEnvs(next, head) {
			break
		}
		head = next
	}

	iteration := a.body(s.Body, head)
	exit := iteration.breaks
	if conditionExit {
		exit = joinViewEnvs(exit, head)
	}
	return viewFlow{normal: exit}
}

func (a *viewAnalysis) rangeStmt(s *Stmt, env viewEnv) viewFlow {
	entry := cloneViewEnv(env)
	if s.IdxName != "" && s.IdxName != "_" {
		delete(entry, s.IdxName)
	}
	if s.ValName != "" && s.ValName != "_" {
		delete(entry, s.ValName)
	}
	if a.zeroRange(s.Value) {
		return viewFlow{normal: entry}
	}
	head := cloneViewEnv(entry)
	for {
		iteration := a.body(s.Body, head)
		back := joinViewEnvs(iteration.normal, iteration.continues)
		next := joinViewEnvs(entry, back)
		if equalViewEnvs(next, head) {
			break
		}
		head = next
	}
	iteration := a.body(s.Body, head)
	return viewFlow{normal: joinViewEnvs(head, iteration.breaks)}
}

func (a *viewAnalysis) switchStmt(s *Stmt, env viewEnv) viewFlow {
	if selected, known := a.constantSwitchCase(s); known {
		if selected < len(s.Cases) {
			branch := a.body(s.Cases[selected].Body, env)
			branch.normal = joinViewEnvs(branch.normal, branch.breaks)
			branch.breaks = nil
			return branch
		}
		if s.HasDef {
			branch := a.body(s.Default, env)
			branch.normal = joinViewEnvs(branch.normal, branch.breaks)
			branch.breaks = nil
			return branch
		}
		return viewFlow{normal: env}
	}
	var out viewFlow
	for _, cs := range s.Cases {
		branch := a.body(cs.Body, env)
		out.normal = joinViewEnvs(out.normal, branch.normal)
		out.normal = joinViewEnvs(out.normal, branch.breaks)
		out.continues = joinViewEnvs(out.continues, branch.continues)
	}
	if s.HasDef {
		branch := a.body(s.Default, env)
		out.normal = joinViewEnvs(out.normal, branch.normal)
		out.normal = joinViewEnvs(out.normal, branch.breaks)
		out.continues = joinViewEnvs(out.continues, branch.continues)
	} else {
		out.normal = joinViewEnvs(out.normal, env)
	}
	return out
}

func (a *viewAnalysis) constantBool(e *Expr) (bool, bool) {
	if e == nil || !e.IsConst || e.Typ == nil || e.Typ.K != KBool {
		return false, false
	}
	return (&checker{p: a.p}).foldBool(e)
}

func (a *viewAnalysis) constantSwitchCase(s *Stmt) (int, bool) {
	if s.Tag == nil || !s.Tag.IsConst {
		return 0, false
	}
	c := &checker{p: a.p}
	for i, cs := range s.Cases {
		for _, value := range cs.Values {
			if comparison, ok := c.compareConstants(s.Tag, value); !ok {
				return 0, false
			} else if comparison == 0 {
				return i, true
			}
		}
	}
	return len(s.Cases), true
}

func (a *viewAnalysis) zeroRange(e *Expr) bool {
	if e == nil || e.Typ == nil {
		return false
	}
	switch e.Typ.K {
	case KInt:
		value, ok := FoldConst(a.p, e)
		return ok && value.Sign() <= 0
	case KArray:
		return e.Typ.ALenSet && e.Typ.ALenVal == 0
	case KSlice:
		if e.K == "composite" {
			return len(e.Elems) == 0 && len(e.Fields) == 0
		}
		if e.K == "builtin" && e.Name == "make" && len(e.Args) != 0 {
			length, ok := FoldConst(a.p, e.Args[0])
			return ok && length.Sign() == 0
		}
	}
	return false
}

func (a *viewAnalysis) returnStmt(s *Stmt, env viewEnv) {
	if len(s.Values) == 1 && len(a.f.Results) == 2 {
		for i := range a.f.Results {
			a.recordReturn(i, a.value(s.Values[0], i, env))
		}
		return
	}
	for i, value := range s.Values {
		a.recordReturn(i, a.value(value, 0, env))
	}
}

func (a *viewAnalysis) recordReturn(result int, origins viewSet) {
	if result >= len(a.returns) || !viewType(a.f.Results[result]) {
		return
	}
	for origin := range origins {
		if origin.param >= 0 {
			a.returns[result][origin.param] = true
		} else if a.validate {
			panic("a view of a local array must not be returned")
		}
	}
}

func (a *viewAnalysis) bind(env viewEnv, name string, typ *Type, origins viewSet) {
	if name == "_" || !viewType(typ) {
		return
	}
	if a.validate {
		holder, ok := a.declScopes[name]
		if ok {
			for origin := range origins {
				if origin.scope >= 0 && a.strictAncestor(holder, origin.scope) {
					panic("a view of a local array must not outlive the array")
				}
			}
		}
	}
	env[name] = cloneViewSet(origins)
}

func (a *viewAnalysis) strictAncestor(ancestor, scope int) bool {
	for scope = a.parents[scope]; scope >= 0; scope = a.parents[scope] {
		if scope == ancestor {
			return true
		}
	}
	return false
}

func (a *viewAnalysis) value(e *Expr, result int, env viewEnv) viewSet {
	if e == nil {
		return viewSet{}
	}
	switch e.K {
	case "ident":
		return cloneViewSet(env[e.Name])
	case "slice_expr":
		if e.X != nil && e.X.Typ != nil && e.X.Typ.K == KArray {
			return a.storage(e.X, env)
		}
		return a.value(e.X, 0, env)
	case "unary":
		if e.Op == "&" {
			return a.storage(e.X, env)
		}
	case "composite":
		origins := viewSet{}
		for _, elem := range e.Elems {
			origins = joinViewSets(origins, a.value(elem, 0, env))
		}
		for _, field := range e.Fields {
			origins = joinViewSets(origins, a.value(field.Value, 0, env))
		}
		return origins
	case "call":
		results := a.summaries[e.Name]
		if result >= len(results) {
			return viewSet{}
		}
		origins := viewSet{}
		for param := range results[result] {
			if param < len(e.Args) {
				origins = joinViewSets(origins, a.value(e.Args[param], 0, env))
			}
		}
		return origins
	case "builtin":
		if e.Name == "append" && len(e.Args) != 0 {
			return a.value(e.Args[0], 0, env)
		}
	}
	return viewSet{}
}

func (a *viewAnalysis) storage(e *Expr, env viewEnv) viewSet {
	for e != nil {
		switch e.K {
		case "field":
			if e.X != nil && e.X.Typ != nil && e.X.Typ.K == KPtr {
				return a.value(e.X, 0, env)
			}
			e = e.X
		case "index":
			if e.X == nil || e.X.Typ == nil || e.X.Typ.K != KArray {
				return a.value(e.X, 0, env)
			}
			e = e.X
		case "ident":
			if _, global := a.p.VarMap[e.Name]; global {
				return viewSet{}
			}
			if scope, ok := a.declScopes[e.Name]; ok {
				return viewSet{{scope: scope, param: -1}: {}}
			}
			return viewSet{}
		default:
			return viewSet{}
		}
	}
	return viewSet{}
}

func (a *viewAnalysis) validateStmtExprs(s *Stmt, env viewEnv) {
	if !a.validate {
		return
	}
	for _, e := range []*Expr{s.Value, s.Cond, s.Tag} {
		a.validateExpr(e, env)
	}
	for _, e := range s.Lhs {
		a.validateExpr(e, env)
	}
	for _, e := range s.Values {
		a.validateExpr(e, env)
	}
	for _, cs := range s.Cases {
		for _, e := range cs.Values {
			a.validateExpr(e, env)
		}
	}
}

func (a *viewAnalysis) validateExpr(e *Expr, env viewEnv) {
	WalkExpr(e, func(x *Expr) {
		switch x.K {
		case "slice_expr":
			if a.globalRoot(x.X) {
				panic("package-level data cannot be sliced")
			}
		case "unary":
			if x.Op == "&" && a.globalRoot(x.X) {
				panic("& on package-level data is not in the subset")
			}
		case "composite":
			for _, elem := range x.Elems {
				if len(a.value(elem, 0, env)) != 0 {
					panic("a view must not be stored in a field or an element")
				}
			}
			for _, field := range x.Fields {
				if len(a.value(field.Value, 0, env)) != 0 {
					panic("a view must not be stored in a field or an element")
				}
			}
		case "builtin":
			if x.Name == "append" {
				for i, arg := range x.Args[1:] {
					if x.Spread && i == len(x.Args)-2 {
						continue
					}
					if len(a.value(arg, 0, env)) != 0 {
						panic("a view must not be stored in a field or an element")
					}
				}
			}
		}
	})
}

func (a *viewAnalysis) globalRoot(e *Expr) bool {
	for e != nil {
		switch e.K {
		case "field", "index":
			e = e.X
		case "ident":
			_, ok := a.p.VarMap[e.Name]
			return ok
		default:
			return false
		}
	}
	return false
}
