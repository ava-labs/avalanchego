package nftfx

import (
	"github.com/ava-labs/gecko/ids"
)

// ID that this Fx uses when labeled
var (
	ID = ids.NewID([32]byte{'n', 'f', 't', 'f', 'x'})
)

// Factory ...
type Factory struct{}

// New ...
func (f *Factory) New() (interface{}, error) { return &Fx{}, nil }
