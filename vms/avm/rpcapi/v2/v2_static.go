package v2

import (
	"net/http"

	"github.com/ava-labs/avalanchego/vms/avm/static"
	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
)

// V2StaticController defines the apis available to the AVM
type V2StaticController struct {
}

// NewStaticController create a new instance of the Static Controller
func NewStaticController() *V2StaticController {
	return &V2StaticController{}
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (c *V2StaticController) BuildGenesis(_ *http.Request, args *vmargs.BuildGenesisArgs, reply *vmargs.BuildGenesisReply) error {
	return static.BuildGenesis(args, reply)
}
