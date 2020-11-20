package v2

import (
	"net/http"

	"github.com/ava-labs/avalanchego/vms/avm/vmargs"

	"github.com/ava-labs/avalanchego/vms/avm/service"
)

// Controller defines the apis available to the AVM
type V2Controller struct {
	service *service.Service
}

// NewController create a new instance of the Controller
func NewController(service *service.Service) *V2Controller {
	return &V2Controller{service: service}
}

// GetTxStatus returns the status of the specified transaction
func (c *V2Controller) GetUTXOs(_ *http.Request, args *vmargs.GetUTXOsArgs, reply *vmargs.GetUTXOsReply) error {
	c.service.Log().Info("AVM: GetUTXOs called for with %s", args.Addresses)

	//TODO Check for nil args ?
	// Most of these will be validated by the service

	var err error
	reply, err = c.service.GetUTXOs(args)
	if err != nil {
		return err
	}

	return nil
}
