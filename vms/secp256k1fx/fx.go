// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

const defaultCacheSize = 256

var (
	ErrWrongVMType                    = errors.New("wrong vm type")
	ErrWrongTxType                    = errors.New("wrong tx type")
	ErrWrongOpType                    = errors.New("wrong operation type")
	ErrWrongUTXOType                  = errors.New("wrong utxo type")
	ErrWrongInputType                 = errors.New("wrong input type")
	ErrWrongCredentialType            = errors.New("wrong credential type")
	ErrWrongOwnerType                 = errors.New("wrong owner type")
	ErrMismatchedAmounts              = errors.New("utxo amount and input amount are not equal")
	ErrWrongNumberOfUTXOs             = errors.New("wrong number of utxos for the operation")
	ErrWrongMintCreated               = errors.New("wrong mint output created from the operation")
	ErrTimelocked                     = errors.New("output is time locked")
	ErrTooManySigners                 = errors.New("input has more signers than expected")
	ErrTooFewSigners                  = errors.New("input has less signers than expected")
	ErrInputOutputIndexOutOfBounds    = errors.New("input referenced a nonexistent address in the output")
	ErrInputCredentialSignersMismatch = errors.New("input expected a different number of signers than provided in the credential")
	ErrWrongSig                       = errors.New("wrong signature")
)

// Fx describes the secp256k1 feature extension
type Fx struct {
	VM           VM
	bootstrapped bool
	recoverCache *secp256k1.RecoverCache
}

func (fx *Fx) Initialize(vmIntf interface{}) error {
	if err := fx.InitializeVM(vmIntf); err != nil {
		return err
	}

	log := fx.VM.Logger()
	log.Debug("initializing secp256k1 fx")

	fx.recoverCache = secp256k1.NewRecoverCache(defaultCacheSize)
	c := fx.VM.CodecRegistry()
	return errors.Join(
		c.RegisterType(&TransferInput{}),
		c.RegisterType(&MintOutput{}),
		c.RegisterType(&TransferOutput{}),
		c.RegisterType(&MintOperation{}),
		c.RegisterType(&Credential{}),
	)
}

func (fx *Fx) InitializeVM(vmIntf interface{}) error {
	vm, ok := vmIntf.(VM)
	if !ok {
		return ErrWrongVMType
	}
	fx.VM = vm
	return nil
}

func (*Fx) Bootstrapping() error {
	return nil
}

func (fx *Fx) Bootstrapped() error {
	fx.bootstrapped = true
	return nil
}

// VerifyPermission returns nil iff [credIntf] proves that [controlGroup] assents to [txIntf]
func (fx *Fx) VerifyPermission(txIntf, inIntf, credIntf, ownerIntf interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	in, ok := inIntf.(*Input)
	if !ok {
		return ErrWrongInputType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return ErrWrongCredentialType
	}
	owner, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return ErrWrongOwnerType
	}
	if err := verify.All(in, cred, owner); err != nil {
		return err
	}
	return fx.VerifyCredentials(tx, in, cred, owner)
}

func (fx *Fx) VerifyOperation(txIntf, opIntf, credIntf interface{}, utxosIntf []interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	op, ok := opIntf.(*MintOperation)
	if !ok {
		return ErrWrongOpType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return ErrWrongCredentialType
	}
	if len(utxosIntf) != 1 {
		return ErrWrongNumberOfUTXOs
	}
	out, ok := utxosIntf[0].(*MintOutput)
	if !ok {
		return ErrWrongUTXOType
	}
	return fx.verifyOperation(tx, op, cred, out)
}

func (fx *Fx) verifyOperation(tx UnsignedTx, op *MintOperation, cred *Credential, utxo *MintOutput) error {
	if err := verify.All(op, cred, utxo); err != nil {
		return err
	}
	if !utxo.Equals(&op.MintOutput.OutputOwners) {
		return ErrWrongMintCreated
	}
	return fx.VerifyCredentials(tx, &op.MintInput, cred, &utxo.OutputOwners)
}

func (fx *Fx) VerifyTransfer(txIntf, inIntf, credIntf, utxoIntf interface{}) error {
	tx, ok := txIntf.(UnsignedTx)
	if !ok {
		return ErrWrongTxType
	}
	in, ok := inIntf.(*TransferInput)
	if !ok {
		return ErrWrongInputType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return ErrWrongCredentialType
	}
	out, ok := utxoIntf.(*TransferOutput)
	if !ok {
		return ErrWrongUTXOType
	}
	return fx.VerifySpend(tx, in, cred, out)
}

// VerifySpend ensures that the utxo can be sent to any address
func (fx *Fx) VerifySpend(utx UnsignedTx, in *TransferInput, cred *Credential, utxo *TransferOutput) error {
	if err := verify.All(utxo, in, cred); err != nil {
		return err
	} else if utxo.Amt != in.Amt {
		return fmt.Errorf("%w: %d != %d", ErrMismatchedAmounts, utxo.Amt, in.Amt)
	}

	return fx.VerifyCredentials(utx, &in.Input, cred, &utxo.OutputOwners)
}

// VerifyCredentials ensures that the output can be spent by the input with the
// credential. A nil return values means the output can be spent.
func (fx *Fx) VerifyCredentials(utx UnsignedTx, in *Input, cred *Credential, out *OutputOwners) error {
	numSigs := len(in.SigIndices)
	switch {
	case out.Locktime > fx.VM.Clock().Unix():
		return ErrTimelocked
	case out.Threshold < uint32(numSigs):
		return ErrTooManySigners
	case out.Threshold > uint32(numSigs):
		return ErrTooFewSigners
	case numSigs != len(cred.Sigs):
		return ErrInputCredentialSignersMismatch
	case !fx.bootstrapped: // disable signature verification during bootstrapping
		return nil
	}

	txHash := hashing.ComputeHash256(utx.Bytes())
	for i, index := range in.SigIndices {
		// Make sure the input references an address that exists
		if index >= uint32(len(out.Addrs)) {
			return ErrInputOutputIndexOutOfBounds
		}
		// Make sure each signature in the signature list is from an owner of
		// the output being consumed
		sig := cred.Sigs[i]
		pk, err := fx.recoverCache.RecoverPublicKeyFromHash(txHash, sig[:])
		if err != nil {
			return err
		}
		if expectedAddress := out.Addrs[index]; expectedAddress != pk.Address() {
			return fmt.Errorf("%w: expected signature from %s but got from %s",
				ErrWrongSig,
				expectedAddress,
				pk.Address())
		}
	}

	return nil
}

// CreateOutput creates a new output with the provided control group worth
// the specified amount
func (*Fx) CreateOutput(amount uint64, ownerIntf interface{}) (interface{}, error) {
	owner, ok := ownerIntf.(*OutputOwners)
	if !ok {
		return nil, ErrWrongOwnerType
	}
	if err := owner.Verify(); err != nil {
		return nil, err
	}
	return &TransferOutput{
		Amt:          amount,
		OutputOwners: *owner,
	}, nil
}
