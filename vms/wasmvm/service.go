package wasmvm

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ava-labs/gecko/utils/formatting"

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

// ArgAPI is the API repr of a function argument
type ArgAPI struct {
	Type  string      `json:"type"`
	Value interface{} `json:"value"`
}

// Return argument as its go type
func (arg *ArgAPI) toFnArg() (interface{}, error) {
	switch strings.ToLower(arg.Type) {
	case "int32":
		if valInt32, ok := arg.Value.(int32); ok {
			return valInt32, nil
		}
		if valInt64, ok := arg.Value.(int64); ok {
			return int32(valInt64), nil
		}
		if valFloat32, ok := arg.Value.(float32); ok {
			return int32(valFloat32), nil
		}
		if valFloat64, ok := arg.Value.(float64); ok {
			return int32(valFloat64), nil
		}
		return nil, fmt.Errorf("value '%v' is not convertible to int32", arg.Value)
	case "int64":
		if valInt32, ok := arg.Value.(int32); ok {
			return int64(valInt32), nil
		}
		if valInt64, ok := arg.Value.(int64); ok {
			return valInt64, nil
		}
		if valFloat32, ok := arg.Value.(float32); ok {
			return int64(valFloat32), nil
		}
		if valFloat64, ok := arg.Value.(float64); ok {
			return int64(valFloat64), nil
		}
		return nil, fmt.Errorf("value '%v' is not convertible to int64", arg.Value)
	default:
		return nil, errors.New("arg type must be one of: int32, int64")
	}
}

// InvokeArgs ...
type InvokeArgs struct {
	ContractID ids.ID          `json:"contractID"`
	Function   string          `json:"function"`
	Args       []ArgAPI        `json:"args"`
	ByteArgs   formatting.CB58 `json:"byteArgs"`
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

	fnArgs := make([]interface{}, len(args.Args))
	var err error
	for i, arg := range args.Args {
		fnArgs[i], err = arg.toFnArg()
		if err != nil {
			return fmt.Errorf("couldn't parse arg '%+v': %s", arg, err)
		}
	}

	tx, err := s.vm.newInvokeTx(args.ContractID, args.Function, fnArgs, args.ByteArgs.Bytes)
	if err != nil {
		return fmt.Errorf("error creating tx: %s", err)
	}

	// Add tx to mempool
	s.vm.mempool = append(s.vm.mempool, tx)
	s.vm.NotifyBlockReady()

	response.TxIssued = true
	return nil
}
