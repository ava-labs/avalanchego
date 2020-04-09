package wasmvm

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
)

// CreateHandlers returns a map where:
// * keys are API endpoint extensions
// * values are API handlers
// See API documentation for more information
func (vm *VM) CreateHandlers() map[string]*common.HTTPHandler {
	handler := vm.SnowmanVM.NewHandler("wasm", &Service{vm: vm})
	return map[string]*common.HTTPHandler{"": handler}
}

// Service is the API service
type Service struct {
	vm *VM
}

/*
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
*/

// InvokeArgs ...
type InvokeArgs struct {
	ContractID ids.ID        `json:"contractID"`
	Function   string        `json:"function"`
	Args       []interface{} `json:"args"`
}

func (args *InvokeArgs) validate() error {
	/*
		if args.ContractID.Equals(ids.Empty) {
			return errors.New("contractID not specified")
		}
	*/
	if args.Function == "" {
		return errors.New("function not specified")
	}
	return nil
}

// InvokeResponse ...
type InvokeResponse struct {
	TxIssued bool `json:"txIssued"`
}

// Invoke ...
func (s *Service) Invoke(_ *http.Request, args *InvokeArgs, response *InvokeResponse) error {
	s.vm.Ctx.Log.Debug("in invoke")
	if err := args.validate(); err != nil {
		return fmt.Errorf("arguments failed validation: %v", err)
	}

	tx, err := s.vm.newInvokeTx(args.ContractID, args.Function, args.Args)
	if err != nil {
		return fmt.Errorf("error creating tx: %s", err)
	}

	// Add tx to mempool
	s.vm.mempool = append(s.vm.mempool, tx)
	s.vm.NotifyBlockReady()

	response.TxIssued = true
	return nil
}
