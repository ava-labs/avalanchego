package wasmvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
)

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	handler := vm.SnowmanVM.NewHandler("salesforce", &Service{vm: vm})
	return map[string]*common.HTTPHandler{"": handler}
}

// Service is the API service
type Service struct {
	vm *VM
}

// ArgAPI is the API repr of a function argument
type ArgAPI struct {
	Type  string
	Value interface{}
}

func (arg *ArgAPI) toArg() (Argument, error) {
	switch strings.ToLower(arg.Type) {
	case "string":
		s, ok := arg.Value.(string)
		if !ok {
			return Argument{}, errors.New("arg type is string but value given is not a string")
		}
		return Argument{
			Type:  String,
			Value: s,
		}, nil
	case "int":
		i, ok := arg.Value.(int)
		if !ok {
			return Argument{}, errors.New("arg type is int but value given is not an int")
		}
		return Argument{
			Type:  String,
			Value: i,
		}, nil
	default:
		return Argument{}, errors.New("arg type must be int or string")
	}
}

// InvokeArgs ...
type InvokeArgs struct {
	ContractID ids.ID   `json:"contractID"`
	Function   string   `json:"function"`
	Args       []ArgAPI `json:"args"`
}

// InvokeResponse ...
type InvokeResponse struct {
	TxIssued bool `json:"txIssued"`
}

// Invoke ...
func (s *Service) Invoke(_ *http.Request, args *InvokeArgs, response *InvokeResponse) error {
	s.vm.Ctx.Log.Debug("in invoke")
	var fnArgs []Argument
	for _, arg := range args.Args {
		fnArg, err := arg.toArg()
		if err != nil {
			return fmt.Errorf("bad argument: %v", err)
		}
		fnArgs = append(fnArgs, fnArg)
	}

	tx, err := s.vm.newInvokeTx(args.ContractID, args.Function, fnArgs)
	if err != nil {
		return fmt.Errorf("error creating tx: %s", err)
	}

	// Add tx to mempool
	s.vm.mempool = append(s.vm.mempool, tx)
	s.vm.NotifyBlockReady()

	response.TxIssued = true
	return nil
}
