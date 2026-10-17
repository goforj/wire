package wire

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// processSlice checks collection inputs without registering element results as ordinary providers.
func (oc *objectCache) processSlice(info *types.Info, pkgPath string, call *ast.CallExpr) (*SliceProvider, []error) {
	if len(call.Args) == 0 {
		return nil, []error{notePosition(oc.fset.Position(call.Pos()), errors.New("wire.Slice requires a pointer to a slice type"))}
	}
	ptr, ok := types.Unalias(info.TypeOf(call.Args[0])).(*types.Pointer)
	if !ok {
		return nil, []error{notePosition(oc.fset.Position(call.Args[0].Pos()), errors.New("first argument to wire.Slice must be a pointer to a slice type"))}
	}
	slice, ok := ptr.Elem().Underlying().(*types.Slice)
	if !ok {
		return nil, []error{notePosition(oc.fset.Position(call.Args[0].Pos()), errors.New("first argument to wire.Slice must be a pointer to a slice type"))}
	}
	collection := &SliceProvider{Pos: call.Pos(), Out: ptr.Elem()}
	var errs []error
	for _, arg := range call.Args[1:] {
		item, itemErrs := oc.processExpr(info, pkgPath, arg, "")
		if len(itemErrs) > 0 {
			errs = append(errs, itemErrs...)
			continue
		}
		p, ok := item.(*Provider)
		if !ok || p.IsStruct {
			errs = append(errs, notePosition(oc.fset.Position(arg.Pos()), errors.New("wire.Slice elements must be provider functions")))
			continue
		}
		if !types.AssignableTo(p.Out[0], slice.Elem()) {
			errs = append(errs, notePosition(oc.fset.Position(arg.Pos()), fmt.Errorf("wire.Slice provider %s returns %s, which is not assignable to %s", p.Name, p.Out[0], slice.Elem())))
			continue
		}
		collection.Providers = append(collection.Providers, p)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return collection, nil
}

// buildSliceProviderMap expands collection contributions in declaration order without mutating imported sets.
func buildSliceProviderMap(fset *token.FileSet, set *ProviderSet, providers, sources *typeutil.Map) []error {
	if len(set.sliceInputs) == 0 {
		return nil
	}
	var errs []error
	set.sliceOutputs = nil
	add := func(contribution *SliceProvider, source *providerSetSrc) {
		var collection *SliceProvider
		if existing := providers.At(contribution.Out); existing != nil {
			collection = existing.(*ProvidedType).s
			if collection == nil {
				errs = append(errs, bindingConflictError(fset, contribution.Out, set, source, sources.At(contribution.Out).(*providerSetSrc)))
				return
			}
		} else {
			collection = &SliceProvider{Pos: contribution.Pos, Out: contribution.Out}
			providers.Set(collection.Out, &ProvidedType{t: collection.Out, s: collection})
			sources.Set(collection.Out, source)
			set.sliceOutputs = append(set.sliceOutputs, collection.Out)
		}
		collection.Providers = append(collection.Providers, contribution.Providers...)
		collection.sources = append(collection.sources, source)
	}
	for _, input := range set.sliceInputs {
		if input.Slice != nil {
			add(input.Slice, input)
			continue
		}
		for _, out := range input.Import.sliceOutputs {
			add(input.Import.For(out).s, input)
		}
	}
	// Imported interface bindings must see added entries, including their cycle edges.
	providers.Iterate(func(typ types.Type, value interface{}) {
		pt := value.(*ProvidedType)
		if pt.s != nil && !types.Identical(typ, pt.t) {
			providers.Set(typ, providers.At(pt.t))
		}
	})
	return errs
}

// providerCall preserves ordinary constructor lifecycle behavior for collection elements.
func providerCall(p *Provider, args []int, out types.Type) call {
	c := call{
		kind:           funcProviderCall,
		pkg:            p.Pkg,
		name:           p.Name,
		methodExprRecv: p.MethodExprRecv,
		args:           args,
		varargs:        p.Varargs,
		out:            out,
		hasCleanup:     p.HasCleanup,
		hasErr:         p.HasErr,
		ins:            make([]types.Type, len(p.Args)),
	}
	for i, arg := range p.Args {
		c.ins[i] = arg.Type
		if p.IsStruct {
			c.fieldNames = append(c.fieldNames, arg.FieldName)
		}
	}
	if p.IsStruct {
		c.kind = structProvider
	}
	return c
}

// sliceExpr emits an ordinary slice literal with resolved elements in contribution order.
func (ig *injectorGen) sliceExpr(lname string, c *call) {
	ig.p("\t%s := %s{", lname, types.TypeString(c.out, ig.g.qualifyPkg))
	for i, arg := range c.args {
		if i > 0 {
			ig.p(", ")
		}
		ig.p("%s", ig.localNames[arg-len(ig.paramNames)])
	}
	ig.p("}\n")
}
