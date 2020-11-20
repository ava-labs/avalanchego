package v1

import (
	"net/http"

	"github.com/ava-labs/avalanchego/vms/avm/internalavm"

	"github.com/ava-labs/avalanchego/vms/avm/vmargs"
)

// Controller defines the apis available to the AVM
type V1Controller struct {
	service *internalavm.Service
}

// NewController create a new instance of the Controller
func NewController(vm *internalavm.VM) *V1Controller {
	return &V1Controller{service: internalavm.NewService(vm)}
}

// GetTxStatus returns the status of the specified transaction
func (c *V1Controller) GetUTXOs(r *http.Request, args *vmargs.GetUTXOsArgs, reply *vmargs.GetUTXOsReply) error {
	return c.service.GetUTXOs(r, args, reply)
}
