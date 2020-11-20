package v1

import (
	"net/http"

	"github.com/ava-labs/avalanchego/vms/avm/internalvm"

	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
)

// Controller defines the apis available to the AVM
type V1Controller struct {
	service *internalvm.Service
}

// NewController create a new instance of the Controller
func NewController(vm *internalvm.VM) *V1Controller {
	return &V1Controller{service: internalvm.NewService(vm)}
}

// GetTxStatus returns the status of the specified transaction
func (c *V1Controller) GetUTXOs(r *http.Request, args *vmargs.GetUTXOsArgs, reply *vmargs.GetUTXOsReply) error {
	return c.service.GetUTXOs(r, args, reply)
}
