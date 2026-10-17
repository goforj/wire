//go:build wireinject

package main

import (
	"example.com/bar"
	"github.com/goforj/wire"
)

// nested exercises collection order across inline and imported sets.
var nested = wire.NewSet(
	wire.Slice(new([]bar.Handler), bar.NewA),
	wire.NewSet(bar.Set, wire.Slice(new([]bar.Handler), bar.NewConcrete)),
)

// namedSet binds a named slice before another set adds more elements to it.
var namedSet = wire.NewSet(
	wire.Slice(new(Handlers), bar.NewCounter, bar.NewCounter),
	wire.Bind(new(Named), new(Handlers)),
)

// injectHandlers preserves each contribution while sharing the ordinary dependency.
func injectHandlers(state *bar.State) ([]bar.Handler, func(), error) {
	wire.Build(bar.NewShared, bar.NewLate, nested, wire.Slice(new([]bar.Handler), bar.NewA))
	return nil, nil, nil
}

// injectNamed extends a collection whose interface binding comes from an imported set.
func injectNamed(state *bar.State) Named {
	wire.Build(namedSet, wire.Slice(new(Handlers), bar.NewCounter))
	return nil
}

// injectEmpty supplies a non-nil empty collection without constructors.
func injectEmpty() []string {
	wire.Build(wire.Slice(new([]string)))
	return nil
}

// injectMethod uses a receiver and a variadic argument from the injector.
func injectMethod(builder *Builder, values []string) []bar.Handler {
	wire.Build(wire.Slice(new([]bar.Handler), (*Builder).New))
	return nil
}

// injectCount consumes the assembled slice as an ordinary dependency.
func injectCount(state *bar.State) int {
	wire.Build(count, wire.Slice(new([]bar.Handler), bar.NewCounter, bar.NewCounter))
	return 0
}

// counterSet contributes a constructor each time the slice-only set is imported.
var counterSet = wire.NewSet(wire.Slice(new([]bar.Handler), bar.NewCounter))

// injectRepeated preserves repeated imports as separate collection contributions.
func injectRepeated(state *bar.State) []bar.Handler {
	wire.Build(counterSet, counterSet)
	return nil
}

// injectAlias merges contributions whose declared types are identical through an alias.
func injectAlias(state *bar.State) []bar.Handler {
	wire.Build(wire.Slice(new(HandlerAlias), bar.NewCounter), wire.Slice(new([]bar.Handler), bar.NewCounter))
	return nil
}

// injectComposition shares a resolved collection between ordinary consumers.
func injectComposition(state *bar.State) *composition {
	wire.Build(count, names, counterSet, wire.Struct(new(composition), "*"))
	return nil
}

// injectSeparate creates the collection element separately from its ordinary binding.
func injectSeparate(state *bar.State) *separate {
	wire.Build(counterSet, bar.NewCounter, wire.Struct(new(separate), "*"))
	return nil
}

// injectSingle verifies that expanding a repeated import did not mutate the shared set.
func injectSingle(state *bar.State) []bar.Handler {
	wire.Build(counterSet)
	return nil
}
