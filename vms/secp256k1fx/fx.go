// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errWrongVMType         = errors.New("wrong vm type")
	errWrongTxType         = errors.New("wrong tx type")
	errWrongOpType         = errors.New("wrong operation type")
	errWrongUTXOType       = errors.New("wrong utxo type")
	errWrongOutputType     = errors.New("wrong output type")
	errWrongInputType      = errors.New("wrong input type")
	errWrongCredentialType = errors.New("wrong credential type")
	errWrongOwnerType      = errors.New("wrong owner type")

	errWrongNumberOfUTXOs = errors.New("wrong number of utxos for the operation")

	errWrongMintCreated               = errors.New("wrong mint output created from the operation")
	errWrongAmounts                   = errors.New("input is consuming a different amount than expected")
	errTimelocked                     = errors.New("output is time locked")
	errTooManySigners                 = errors.New("input has more signers than expected")
	errTooFewSigners                  = errors.New("input has less signers than expected")
	errInputCredentialSignersMismatch = errors.New("input expected a different number of signers than provided in the credential")
	errWrongSigner                    = errors.New("credential does not produce expected signer")
)

// Fx describes the secp256k1 feature extension
type Fx struct {
	VM           VM
	SECPFactory  crypto.FactorySECP256K1R
	bootstrapped bool
}

// Initialize ...
func (fx *Fx) Initialize(vmIntf interface{}) error {
	if err := fx.InitializeVM(vmIntf); err != nil {
		return err
	}

	log := fx.VM.Logger()
	log.Debug("Initializing secp561k1 fx")

	c := fx.VM.Codec()
	errs := wrappers.Errs{}
	errs.Add(
		c.RegisterType(&TransferInput{}),
		c.RegisterType(&MintOutput{}),
		c.RegisterType(&TransferOutput{}),
		c.RegisterType(&MintOperation{}),
		c.RegisterType(&Credential{}),
	)
	return errs.Err
}

// InitializeVM ...
func (fx *Fx) InitializeVM(vmIntf interface{}) error {
	vm, ok := vmIntf.(VM)
	if !ok {
		return errWrongVMType
	}
	fx.VM = vm
	return nil
}

// Bootstrapping ...
func (fx *Fx) Bootstrapping() error { return nil }

// Bootstrapped ...
func (fx *Fx) Bootstrapped() error { fx.bootstrapped = true; return nil }

// VerifyPermission returns nil iff [credIntf] proves that [controlGroup] assents to [txIntf]
func (fx *Fx) VerifyPermission(txIntf, credIntf, controlGroup interface{}) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errWrongTxType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return errWrongCredentialType
	}
	owner, ok := controlGroup.(*OutputOwners)
	if !ok {
		return errWrongOwnerType
	}
	return fx.verifyPermission(tx, cred, owner)
}

func (fx *Fx) verifyPermission(tx Tx, cred *Credential, owners *OutputOwners) error {
	if err := verify.All(cred, owners); err != nil { // Make sure cred and control group are well-formed
		return err
	} else if len(cred.Sigs) != int(owners.Threshold) {
		return fmt.Errorf("credential has %d signatures but should have %d", len(cred.Sigs), owners.Threshold)
	}
	txHash := hashing.ComputeHash256(tx.UnsignedBytes())
	controlAddrs := owners.AddressesSet()
	seen := ids.ShortSet{} // addresses we've already seen sigs from
	for _, sig := range cred.Sigs {
		if pubKey, err := fx.SECPFactory.RecoverHashPublicKey(txHash, sig[:]); err != nil {
			return err
		} else if addr := pubKey.Address(); !controlAddrs.Contains(addr) {
			return fmt.Errorf("credential has signature from %s, which is not in the control group", addr)
		} else if seen.Contains(addr) {
			return fmt.Errorf("signature from %s appears multiple times", addr)
		} else {
			seen.Add(addr)
		}
	}
	return nil
}

// VerifyOperation ...
func (fx *Fx) VerifyOperation(txIntf, opIntf, credIntf interface{}, utxosIntf []interface{}) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errWrongTxType
	}
	op, ok := opIntf.(*MintOperation)
	if !ok {
		return errWrongOpType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return errWrongCredentialType
	}
	if len(utxosIntf) != 1 {
		return errWrongNumberOfUTXOs
	}
	out, ok := utxosIntf[0].(*MintOutput)
	if !ok {
		return errWrongUTXOType
	}
	return fx.verifyOperation(tx, op, cred, out)
}

func (fx *Fx) verifyOperation(tx Tx, op *MintOperation, cred *Credential, utxo *MintOutput) error {
	if err := verify.All(op, cred, utxo); err != nil {
		return err
	}
	if !utxo.Equals(&op.MintOutput.OutputOwners) {
		return errWrongMintCreated
	}
	return fx.VerifyCredentials(tx, &op.MintInput, cred, &utxo.OutputOwners)
}

// VerifyTransfer ...
func (fx *Fx) VerifyTransfer(txIntf, inIntf, credIntf, utxoIntf interface{}) error {
	tx, ok := txIntf.(Tx)
	if !ok {
		return errWrongTxType
	}
	in, ok := inIntf.(*TransferInput)
	if !ok {
		return errWrongInputType
	}
	cred, ok := credIntf.(*Credential)
	if !ok {
		return errWrongCredentialType
	}
	out, ok := utxoIntf.(*TransferOutput)
	if !ok {
		return errWrongUTXOType
	}
	return fx.VerifySpend(tx, in, cred, out)
}

// VerifySpend ensures that the utxo can be sent to any address
func (fx *Fx) VerifySpend(tx Tx, in *TransferInput, cred *Credential, utxo *TransferOutput) error {
	if err := verify.All(utxo, in, cred); err != nil {
		return err
	} else if utxo.Amt != in.Amt {
		return errWrongAmounts
	}

	return fx.VerifyCredentials(tx, &in.Input, cred, &utxo.OutputOwners)
}

// VerifyCredentials ensures that the output can be spent by the input with the
// credential. A nil return values means the output can be spent.
func (fx *Fx) VerifyCredentials(tx Tx, in *Input, cred *Credential, out *OutputOwners) error {
	numSigs := len(in.SigIndices)
	switch {
	case out.Locktime > fx.VM.Clock().Unix():
		return errTimelocked
	case out.Threshold < uint32(numSigs):
		return errTooManySigners
	case out.Threshold > uint32(numSigs):
		return errTooFewSigners
	case numSigs != len(cred.Sigs):
		return errInputCredentialSignersMismatch
	case !fx.bootstrapped: // disable signature verification during bootstrapping
		return nil
	}

	txHash := hashing.ComputeHash256(tx.UnsignedBytes())
	for i, index := range in.SigIndices {
		// Make sure each signature in the signature list is from
		// an owner of the output being consumed
		sig := cred.Sigs[i]
		pk, err := fx.SECPFactory.RecoverHashPublicKey(txHash, sig[:])
		if err != nil {
			return err
		}
		if expectedAddress := out.Addrs[index]; !expectedAddress.Equals(pk.Address()) {
			return errWrongSigner
		}
	}

	return nil
}
