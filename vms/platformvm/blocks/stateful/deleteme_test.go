package stateful

import "testing"

type a struct {
	freeer
}

func (a a) free(t *testing.T) {
	a.freeer.freeA(t, a)
}

func (b b) free(t *testing.T) {
	b.freeer.freeB(t, b)
}

type b struct{ a }

type freeer interface {
	freeA(*testing.T, a)
	freeB(*testing.T, b)
}

type freeerImpl struct{}

func (f freeerImpl) freeA(t *testing.T, a a) { t.Fatal("a") }
func (f freeerImpl) freeB(t *testing.T, b b) { t.Fatal("b") }

type letter interface {
	thing()
}

func TestFoo(t *testing.T) {
	f := &freeerImpl{}
	b := b{a{f}}
	b.free(t)
}
