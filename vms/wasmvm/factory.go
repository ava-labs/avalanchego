package wasmvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/core"
)

// ID of the Wasm VM
var (
	ID = ids.NewID([32]byte{'w', 'a', 's', 'm'})
)

// Factory can create new instances of the Wasm Chain
type Factory struct{}

// New returns a new instance of the Wasm Chain
func (f *Factory) New() interface{} {
	return &VM{
		SnowmanVM: &core.SnowmanVM{},
	}
}
