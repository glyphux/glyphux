// Invariant 2 of PRD §17.2's boundary-verify gate: no domain-API
// implementation exposes a raw DB/FS handle across the boundary. Unlike the
// import-statement check in imports_test.go (a text-level proxy for "does
// this code touch database/sql at all"), this check inspects exported
// function/method *signatures* directly via go/types, so it also catches a
// domain API that imports database/sql legitimately (e.g. for error
// sentinels) but leaks a raw *sql.Rows or *os.File through an otherwise
// clean-looking export.
package boundary

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// domainAPIPackages are the domain-API surfaces subject to the
// Communication Law (§5.2): kernel domain APIs must never leak a raw
// database or filesystem handle to callers above them. internal/pluginstore
// was added by Phase-5 Ticket T1 (the SQL-backed plugin KV backend): it is
// exactly the kind of db-touching store this invariant exists for, and its
// only exported surface is NewStore(db.Queryer) + Get/Set/Delete — no raw
// handle may cross it.
var domainAPIPackages = []string{
	"github.com/glyphux/glyphux/internal/content",
	"github.com/glyphux/glyphux/internal/composition",
	"github.com/glyphux/glyphux/internal/media",
	"github.com/glyphux/glyphux/internal/identity",
	"github.com/glyphux/glyphux/internal/pluginstore",
}

// disallowedHandleTypes are driver/FS-native types that must never appear in
// an exported domain-API function or method signature (as a parameter or a
// result), keyed by (import path, type name).
var disallowedHandleTypes = map[string]map[string]bool{
	"database/sql": {
		"DB":   true,
		"Tx":   true,
		"Rows": true,
		"Row":  true,
		"Stmt": true,
		"Conn": true,
	},
	"os": {
		"File": true,
	},
}

// CheckRawHandleLeakage loads the packages named by patterns (using cfg as
// the load configuration) and reports every exported function or method
// whose signature accepts or returns a raw handle type from
// disallowedHandleTypes, directly or through a pointer/slice/array/map/chan
// wrapper. One violation string is produced per offending parameter or
// result.
func CheckRawHandleLeakage(cfg *packages.Config, patterns ...string) ([]string, error) {
	loadCfg := *cfg
	loadCfg.Mode |= packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo

	pkgs, err := packages.Load(&loadCfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("boundary: loading packages %v: %w", patterns, err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		return nil, fmt.Errorf("boundary: errors loading packages %v (see stderr)", patterns)
	}

	var violations []string
	for _, pkg := range pkgs {
		if pkg.Types == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			if !token.IsExported(name) {
				continue
			}
			switch o := scope.Lookup(name).(type) {
			case *types.Func:
				violations = append(violations, checkSignature(pkg.PkgPath, name, o.Type())...)
			case *types.TypeName:
				named, ok := o.Type().(*types.Named)
				if !ok {
					continue
				}
				for i := 0; i < named.NumMethods(); i++ {
					m := named.Method(i)
					if !m.Exported() {
						continue
					}
					violations = append(violations, checkSignature(pkg.PkgPath, name+"."+m.Name(), m.Type())...)
				}
			}
		}
	}
	sort.Strings(violations)
	return violations, nil
}

// checkSignature reports a violation for every parameter or result of t
// (which must be a *types.Signature) whose type resolves to a disallowed
// raw handle type.
func checkSignature(pkgPath, funcName string, t types.Type) []string {
	sig, ok := t.(*types.Signature)
	if !ok {
		return nil
	}

	var violations []string
	report := func(v *types.Var, role string, idx int) {
		importPath, typeName, bad := disallowedType(v.Type())
		if !bad {
			return
		}
		violations = append(violations, fmt.Sprintf(
			"%s.%s: %s #%d has raw handle type %s.%s",
			pkgPath, funcName, role, idx, importPath, typeName,
		))
	}
	if params := sig.Params(); params != nil {
		for i := 0; i < params.Len(); i++ {
			report(params.At(i), "param", i)
		}
	}
	if results := sig.Results(); results != nil {
		for i := 0; i < results.Len(); i++ {
			report(results.At(i), "result", i)
		}
	}
	return violations
}

// disallowedType unwraps pointer/slice/array/chan/map-value wrappers looking
// for a named type listed in disallowedHandleTypes.
func disallowedType(t types.Type) (importPath, name string, bad bool) {
	for {
		switch u := t.(type) {
		case *types.Pointer:
			t = u.Elem()
		case *types.Slice:
			t = u.Elem()
		case *types.Array:
			t = u.Elem()
		case *types.Chan:
			t = u.Elem()
		case *types.Map:
			t = u.Elem()
		case *types.Named:
			obj := u.Obj()
			if obj.Pkg() == nil {
				return "", "", false
			}
			path := obj.Pkg().Path()
			if names, ok := disallowedHandleTypes[path]; ok && names[obj.Name()] {
				return path, obj.Name(), true
			}
			return "", "", false
		default:
			return "", "", false
		}
	}
}
