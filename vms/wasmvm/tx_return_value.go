package wasmvm

import (
	"encoding/json"

	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/formatting"
)

// txReturnValue is a transaction, its status and, if the tx was a SC method invocation,
// its return value.
type txReturnValue struct {
	vm *VM

	// The transaction itself
	Tx tx `serialize:"true" json:"tx"`
	// Status of the transaction
	choices.Status `serialize:"true" json:"status"`
	// True if Tx is an invokeTx and the SC method invocation was successful
	// Otherwise false
	InvocationSuccessful bool `serialize:"true" json:"invocationSuccessful"`
	// If Tx is an invokeTx, ReturnValue is the SC method's return value
	// Otherwise empty.
	ReturnValue []byte `serialize:"true" json:"returned"`
}

// Bytes returns the byte representation
func (rv *txReturnValue) Bytes() []byte {
	bytes, err := codec.Marshal(rv)
	if err != nil {
		rv.vm.Ctx.Log.Error("couldn't marshal TxReturnValue: %v", err)
	}
	return bytes
}

func (rv *txReturnValue) MarshalJSON() ([]byte, error) {
	asMap := make(map[string]interface{}, 6)
	asMap["tx"] = rv.Tx
	asMap["status"] = rv.Status.String()
	switch rv.Tx.(type) {
	case *invokeTx:
		asMap["type"] = "contract invocation"
		asMap["invocationSuccessful"] = rv.InvocationSuccessful
		byteFormatter := formatting.CB58{Bytes: rv.ReturnValue}
		asMap["returnValue"] = byteFormatter.String()
	case *createContractTx:
		asMap["type"] = "contract creation"
	}

	return json.Marshal(asMap)
}
