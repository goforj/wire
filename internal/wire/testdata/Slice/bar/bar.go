package bar

import (
	"errors"
	"fmt"

	"github.com/goforj/wire"
)

// Handler is the shared result type of independent constructors.
type Handler interface{ Name() string }

// State records construction and cleanup order.
type State struct {
	Fail   string
	Events []string
	Next   int
}

// Shared is an ordinary dependency shared by all collection entries.
type Shared struct{ State *State }

// Late is only needed by a later collection entry.
type Late struct{}

// Concrete also satisfies Handler without an interface binding.
type Concrete struct{ name string }

// Name identifies a constructed handler.
func (h *Concrete) Name() string { return h.name }

// NewShared records its lifetime independently of collection element lifetimes.
func NewShared(s *State) (*Shared, func(), error) {
	s.Events = append(s.Events, "dep")
	if s.Fail == "dep" {
		return nil, nil, errors.New("dep")
	}
	return &Shared{State: s}, func() { s.Events = append(s.Events, "clean:dep") }, nil
}

// newHandler makes failures observable before a cleanup is acquired.
func newHandler(d *Shared, name string) (*Concrete, func(), error) {
	s := d.State
	s.Events = append(s.Events, name)
	if s.Fail == name {
		return nil, nil, errors.New(name)
	}
	return &Concrete{name: name}, func() { s.Events = append(s.Events, "clean:"+name) }, nil
}

// NewA returns the same interface type as NewB.
func NewA(d *Shared) (Handler, func(), error) { return newHandler(d, "a") }

// NewB returns the same interface type as NewA.
func NewB(d *Shared) (Handler, func(), error) { return newHandler(d, "b") }

// NewLate exposes ordering and cleanup of a dependency needed by a later entry.
func NewLate(d *Shared) (*Late, func(), error) {
	s := d.State
	s.Events = append(s.Events, "late")
	if s.Fail == "late" {
		return nil, nil, errors.New("late")
	}
	return &Late{}, func() { s.Events = append(s.Events, "clean:late") }, nil
}

// NewConcrete returns a concrete result assignable to the collection element type.
func NewConcrete(d *Shared, late *Late) (*Concrete, func(), error) { return newHandler(d, "c") }

// NewCounter identifies each occurrence of a repeated constructor.
func NewCounter(s *State) *Concrete {
	s.Next++
	return &Concrete{name: fmt.Sprint(s.Next)}
}

// Contribution exercises a collection directive stored in a package variable.
var Contribution = wire.Slice(new([]Handler), NewB)

// Set contributes handlers without exporting an ordinary Handler binding.
var Set = wire.NewSet(Contribution)
