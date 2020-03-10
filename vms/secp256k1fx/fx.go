// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errWrongVMType         = errors.New("wrong vm type")
	errWrongTxType         = errors.New("wrong tx type")
	errWrongUTXOType       = errors.New("wrong utxo type")
	errWrongOutputType     = errors.New("wrong output type")
	errWrongInputType      = errors.New("wrong input type")
	errWrongCredentialType = errors.New("wrong credential type")

	errWrongNumberOfOutputs     = errors.New("wrong number of outputs for an operation")
	errWrongNumberOfInputs      = errors.New("wrong number of inputs for an operation")
	errWrongNumberOfCredentials = errors.New("wrong number of credentials for an operation")

	errWrongMintCreated = errors.New("wrong mint output created from the operation")

	errWrongAmounts                   = errors.New("input is consuming a different amount than expected")
	errTimelocked                     = errors.New("output is time locked")
	errTooManySigners                 = errors.New("input has more signers than expected")
	errTooFewSigners                  = errors.New("input has less signers than expected")
	errInputCredentialSignersMismatch = errors.New("input expected a different number of signers than provided in the credential")
	errWrongSigner                    = errors.New("credential does not produce expected signer")
)

// Fx ...
type Fx struct {
	vm          VM
	secpFactory crypto.FactorySECP256K1R
}

// Initialize ...
func (fx *Fx) Initialize(vmIntf interface{}) error {
	vm, ok := vmIntf.(VM)
	if !ok {
		return errWrongVMType
	}

	c := vm.Codec()
	c.RegisterType(&MintOutput{})
	c.RegisterType(&TransferOutput{})
	c.RegisterType(&MintInput{})
	c.RegisterType(&TransferInput{})
	c.RegisterType(&Credential{})

	fx.vm = vm
	return nil
}

// VerifyOperation ...
func (fx *Fx) VerifyOperation(txIntf interface{}, utxosIntf, insIntf, credsIntf, outsIntf []interface{}) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errWrongTxType
	}

	if len(outsIntf) != 2 {
		return errWrongNumberOfOutputs
	}
	if len(utxosIntf) != 1 || len(insIntf) != 1 {
		return errWrongNumberOfInputs
	}
	if len(credsIntf) != 1 {
		return errWrongNumberOfCredentials
	}

	utxo, ok := utxosIntf[0].(*MintOutput)
	if !ok {
		return errWrongUTXOType
	}
	in, ok := insIntf[0].(*MintInput)
	if !ok {
		return errWrongInputType
	}
	cred, ok := credsIntf[0].(*Credential)
	if !ok {
		return errWrongCredentialType
	}
	newMint, ok := outsIntf[0].(*MintOutput)
	if !ok {
		return errWrongOutputType
	}
	newOutput, ok := outsIntf[1].(*TransferOutput)
	if !ok {
		return errWrongOutputType
	}

	return fx.verifyOperation(tx, utxo, in, cred, newMint, newOutput)
}

func (fx *Fx) verifyOperation(tx Tx, utxo *MintOutput, in *MintInput, cred *Credential, newMint *MintOutput, newOutput *TransferOutput) error {
	if err := verify.All(utxo, in, cred, newMint, newOutput); err != nil {
		return err
	}

	if !utxo.Equals(&newMint.OutputOwners) {
		return errWrongMintCreated
	}

	return fx.verifyCredentials(tx, &utxo.OutputOwners, &in.Input, cred)
}

// VerifyTransfer ...
func (fx *Fx) VerifyTransfer(txIntf, utxoIntf, inIntf, credIntf interface{}) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errWrongTxType
	}
	utxo, ok := utxoIntf.(*TransferOutput)
	if !ok {
		return errWrongUTXOType
	}
	in, ok := inIntf.(*TransferInput)
	if !ok {
		return errWrongInputType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return errWrongCredentialType
	}
	return fx.verifyTransfer(tx, utxo, in, cred)
}

func (fx *Fx) verifyTransfer(tx Tx, utxo *TransferOutput, in *TransferInput, cred *Credential) error {
	if err := verify.All(utxo, in, cred); err != nil {
		return err
	}

	clock := fx.vm.Clock()
	switch {
	case utxo.Amt != in.Amt:
		return errWrongAmounts
	case utxo.Locktime > clock.Unix():
		return errTimelocked
	}

	return fx.verifyCredentials(tx, &utxo.OutputOwners, &in.Input, cred)
}

func (fx *Fx) verifyCredentials(tx Tx, out *OutputOwners, in *Input, cred *Credential) error {
	numSigs := len(in.SigIndices)
	switch {
	case out.Threshold < uint32(numSigs):
		return errTooManySigners
	case out.Threshold > uint32(numSigs):
		return errTooFewSigners
	case numSigs != len(cred.Sigs):
		return errInputCredentialSignersMismatch
	}

	txBytes := tx.UnsignedBytes()
	txHash := hashing.ComputeHash256(txBytes)

	for i, index := range in.SigIndices {
		sig := cred.Sigs[i]

		pk, err := fx.secpFactory.RecoverHashPublicKey(txHash, sig[:])
		if err != nil {
			return err
		}

		expectedAddress := out.Addrs[index]
		if !expectedAddress.Equals(pk.Address()) {
			return errWrongSigner
		}
	}

	return nil
}
