// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tx

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ Credential = (*ContractCredential)(nil)

	errNilContractCredential = errors.New("nil contract credential")
	errUnauthorizedImport    = errors.New("import not authorized in helper storage")
	errImportOwnerMismatch   = errors.New("import recipient is not the single UTXO owner")
	errContractImportShape   = errors.New("contract credentials require a P-chain import with one output")
)

// ContractCredential marks an input authorized by an EVM call to the helper.
// It has no payload. The approval commits to the complete unsigned import.
type ContractCredential struct{}

func (cr *ContractCredential) Verify() error {
	if cr == nil {
		return errNilContractCredential
	}
	return nil
}

func isContractCredential(cred Credential) bool {
	_, ok := cred.(*ContractCredential)
	return ok
}

// ImportAuth supplies the helper's state and the timestamp in Unix seconds.
// Consensus callers MUST supply the containing block's settled state and time.
// The pool can use the latest executed state and local time for admission only.
type ImportAuth struct {
	State     libevm.StateReader
	Helper    common.Address
	Timestamp uint64
}

// ImportApprovalSlot is the slot for authorized[keccak256(unsigned)] in the
// helper's mapping at slot 0. The helper's storage layout is consensus-critical.
func ImportApprovalSlot(unsigned []byte) common.Hash {
	hash := crypto.Keccak256Hash(unsigned)
	return crypto.Keccak256Hash(hash[:], common.Hash{}.Bytes())
}

func (a *ImportAuth) authorized(unsigned []byte) bool {
	return a != nil && a.State != nil && a.Helper != (common.Address{}) &&
		a.State.GetState(a.Helper, ImportApprovalSlot(unsigned)) == (common.Hash{31: 1})
}

func verifyContractTransfer(owner common.Address, timestamp uint64, in *secp256k1fx.TransferInput, cred *ContractCredential, out *secp256k1fx.TransferOutput) error {
	if err := verify.All(in, cred, out); err != nil {
		return err
	}
	if out.Amt != in.Amt {
		return fmt.Errorf("%w: %d != %d", secp256k1fx.ErrMismatchedAmounts, out.Amt, in.Amt)
	}
	if out.Threshold != 1 || len(out.Addrs) != 1 || out.Addrs[0] != ids.ShortID(owner) ||
		len(in.SigIndices) != 1 || in.SigIndices[0] != 0 {
		return errImportOwnerMismatch
	}
	if out.Locktime > timestamp {
		return secp256k1fx.ErrTimelocked
	}
	return nil
}
