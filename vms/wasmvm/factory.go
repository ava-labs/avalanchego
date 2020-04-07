package wasmvm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/core"
)

// ID of the platform VM
var (
	ID = ids.NewID([32]byte{'w', 'a', 's', 'm'})
)

// Factory can create new instances of the Platform Chain
type Factory struct {
}

// New returns a new instance of the Platform Chain
func (f *Factory) New() interface{} {
	return &VM{
		SnowmanVM: &core.SnowmanVM{},
	}
}
