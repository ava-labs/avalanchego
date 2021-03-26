// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/coreth/core/state"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ethereum/go-ethereum/common"
)

var (
	errWrongBlockchainID = errors.New("wrong blockchain ID provided")
	errWrongNetworkID    = errors.New("tx was issued with a different network ID")
	errNilTx             = errors.New("tx is nil")
)

// EVMOutput defines an output from EVM State created from export transactions
type EVMOutput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
}

// EVMInput defines an input for the EVM State to be used in import transactions
type EVMInput struct {
	Address common.Address `serialize:"true" json:"address"`
	Amount  uint64         `serialize:"true" json:"amount"`
	AssetID ids.ID         `serialize:"true" json:"assetID"`
	Nonce   uint64         `serialize:"true" json:"nonce"`
}

// Verify ...
func (out *EVMOutput) Verify() error {
	return nil
}

// Verify ...
func (in *EVMInput) Verify() error {
	return nil
}

// UnsignedTx is an unsigned transaction
type UnsignedTx interface {
	Initialize(unsignedBytes, signedBytes []byte)
	ID() ids.ID
	UnsignedBytes() []byte
	Bytes() []byte
}

// UnsignedAtomicTx is an unsigned operation that can be atomically accepted
type UnsignedAtomicTx interface {
	UnsignedTx

	// UTXOs this tx consumes
	InputUTXOs() ids.Set
	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, stx *Tx, ap1 bool) TxError

	// Accept this transaction with the additionally provided state transitions.
	Accept(ctx *snow.Context, batch database.Batch) error

	EVMStateTransfer(vm *VM, state *state.StateDB) error
}

// Tx is a signed transaction
type Tx struct {
	// The body of this transaction
	UnsignedTx `serialize:"true" json:"unsignedTx"`

	// The credentials of this transaction
	Creds []verify.Verifiable `serialize:"true" json:"credentials"`
}

// (*secp256k1fx.Credential)

// Sign this transaction with the provided signers
func (tx *Tx) Sign(c codec.Manager, signers [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := c.Marshal(codecVersion, &tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedTx: %w", err)
	}

	// Attach credentials
	hash := hashing.ComputeHash256(unsignedBytes)
	for _, keys := range signers {
		cred := &secp256k1fx.Credential{
			Sigs: make([][crypto.SECP256K1RSigLen]byte, len(keys)),
		}
		for i, key := range keys {
			sig, err := key.SignHash(hash) // Sign hash
			if err != nil {
				return fmt.Errorf("problem generating credential: %w", err)
			}
			copy(cred.Sigs[i][:], sig)
		}
		tx.Creds = append(tx.Creds, cred) // Attach credential
	}

	signedBytes, err := c.Marshal(codecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal Tx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

// innerSortInputsAndSigners implements sort.Interface for EVMInput
type innerSortInputsAndSigners struct {
	inputs  []EVMInput
	signers [][]*crypto.PrivateKeySECP256K1R
}

func (ins *innerSortInputsAndSigners) Less(i, j int) bool {
	addrComp := bytes.Compare(ins.inputs[i].Address.Bytes(), ins.inputs[j].Address.Bytes())
	if addrComp != 0 {
		return addrComp < 0
	}
	return bytes.Compare(ins.inputs[i].AssetID[:], ins.inputs[j].AssetID[:]) < 0
}

func (ins *innerSortInputsAndSigners) Len() int { return len(ins.inputs) }

func (ins *innerSortInputsAndSigners) Swap(i, j int) {
	ins.inputs[j], ins.inputs[i] = ins.inputs[i], ins.inputs[j]
	ins.signers[j], ins.signers[i] = ins.signers[i], ins.signers[j]
}

// SortEVMInputsAndSigners sorts the list of EVMInputs based on the addresses and assetIDs
func SortEVMInputsAndSigners(inputs []EVMInput, signers [][]*crypto.PrivateKeySECP256K1R) {
	sort.Sort(&innerSortInputsAndSigners{inputs: inputs, signers: signers})
}

// IsSortedAndUniqueEVMInputs returns true if the EVM Inputs are sorted and unique
// based on the account addresses
func IsSortedAndUniqueEVMInputs(inputs []EVMInput) bool {
	return utils.IsSortedAndUnique(&innerSortInputsAndSigners{inputs: inputs})
}

// innerSortEVMOutputs implements sort.Interface for EVMOutput
type innerSortEVMOutputs struct {
	outputs []EVMOutput
}

func (outs *innerSortEVMOutputs) Less(i, j int) bool {
	addrComp := bytes.Compare(outs.outputs[i].Address.Bytes(), outs.outputs[j].Address.Bytes())
	if addrComp != 0 {
		return addrComp < 0
	}
	return bytes.Compare(outs.outputs[i].AssetID[:], outs.outputs[j].AssetID[:]) < 0
}

func (outs *innerSortEVMOutputs) Len() int { return len(outs.outputs) }

func (outs *innerSortEVMOutputs) Swap(i, j int) {
	outs.outputs[j], outs.outputs[i] = outs.outputs[i], outs.outputs[j]
}

// SortEVMOutputs sorts the list of EVMOutputs based on the addresses and assetIDs
// of the outputs
func SortEVMOutputs(outputs []EVMOutput) {
	sort.Sort(&innerSortEVMOutputs{outputs: outputs})
}

// IsSortedEVMOutputs returns true if the EVMOutputs are sorted
// based on the account addresses and assetIDs
func IsSortedEVMOutputs(outputs []EVMOutput) bool {
	return sort.IsSorted(&innerSortEVMOutputs{outputs: outputs})
}
