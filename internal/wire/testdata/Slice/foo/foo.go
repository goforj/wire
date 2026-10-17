package main

import (
	"fmt"
	"strings"

	"example.com/bar"
)

// Handlers is a named collection that can also satisfy an ordinary interface binding.
type Handlers []bar.Handler

// HandlerAlias exercises contributions using identical aliased and unnamed types.
type HandlerAlias = []bar.Handler

// Names reports collection order without exposing the concrete slice type.
func (h Handlers) Names() string {
	var names []string
	for _, handler := range h {
		names = append(names, handler.Name())
	}
	return strings.Join(names, ",")
}

// Named exposes a method on a named slice.
type Named interface{ Names() string }

// Builder exercises a variadic method expression as an element constructor.
type Builder struct{ Prefix string }

// methodHandler supplies an implementation local to the injector package.
type methodHandler string

// Name identifies a method-produced handler.
func (h methodHandler) Name() string { return string(h) }

// New constructs a handler using receiver and variadic dependencies.
func (b *Builder) New(values ...string) bar.Handler {
	return methodHandler(b.Prefix + strings.Join(values, ","))
}

// count consumes a collection through an ordinary provider dependency.
func count(handlers []bar.Handler) int { return len(handlers) }

// composition consumes the same collection through two ordinary providers.
type composition struct {
	Count int
	Names string
}

// names consumes the shared collection independently of count.
func names(handlers []bar.Handler) string { return Handlers(handlers).Names() }

// separate distinguishes a collection entry from an ordinary binding of its constructor.
type separate struct {
	Collection []bar.Handler
	Individual *bar.Concrete
}

// main exercises generated construction and cleanup rather than only comparing source.
func main() {
	s := &bar.State{}
	handlers, cleanup, err := injectHandlers(s)
	if err != nil {
		panic(err)
	}
	fmt.Println("handlers:", Handlers(handlers).Names())
	fmt.Println("created:", strings.Join(s.Events, ","))
	cleanup()
	fmt.Println("cleaned:", strings.Join(s.Events, ","))
	for _, fail := range []string{"dep", "a", "b", "late", "c"} {
		s := &bar.State{Fail: fail}
		handlers, cleanup, err := injectHandlers(s)
		if err == nil || handlers != nil || cleanup != nil {
			panic("failure did not return zero output and nil cleanup")
		}
		fmt.Printf("%s: %s\n", err, strings.Join(s.Events, ","))
	}
	fmt.Println("named:", injectNamed(&bar.State{}).Names())
	empty := injectEmpty()
	fmt.Printf("empty: %d nil=%t\n", len(empty), empty == nil)
	fmt.Println("method:", injectMethod(&Builder{Prefix: "prefix/"}, []string{"one", "two"})[0].Name())
	fmt.Println("count:", injectCount(&bar.State{}))
	fmt.Println("repeated:", Handlers(injectRepeated(&bar.State{})).Names())
	fmt.Println("alias:", Handlers(injectAlias(&bar.State{})).Names())
	state := &bar.State{}
	composition := injectComposition(state)
	fmt.Printf("shared collection: %d %s calls=%d\n", composition.Count, composition.Names, state.Next)
	separate := injectSeparate(&bar.State{})
	fmt.Printf("separate: %s/%s\n", Handlers(separate.Collection).Names(), separate.Individual.Name())
	fmt.Println("single import:", Handlers(injectSingle(&bar.State{})).Names())
}
