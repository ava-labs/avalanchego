package v1

import (
	"net/http"

	"github.com/ava-labs/avalanchego/vms/avm/internalvm"
	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
)

// Controller defines the apis available to the AVM
type V1StaticController struct {
}

// NewStaticController create a new instance of the Controller
func NewStaticController() *V1StaticController {
	return &V1StaticController{}
}

// BuildGenesis returns the UTXOs such that at least one address in [args.Addresses] is
// referenced in the UTXO.
func (c *V1StaticController) BuildGenesis(_ *http.Request, args *vmargs.BuildGenesisArgs, reply *vmargs.BuildGenesisReply) error {

	staticService, err := internalvm.CreateStaticService(args.Encoding)
	if err != nil {
		// TODO review this - add log ?
		return err
	}

	return staticService.BuildGenesis(nil, args, reply)
}
