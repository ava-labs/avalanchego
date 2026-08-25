// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	_ verify.Verifiable = (*WarpCredential)(nil)

	ErrNilWarpCredential    = errors.New("nil warp credential")
	ErrWrongWarpPayload     = errors.New("warp payload is not the unsigned tx hash")
	ErrWrongWarpSourceAddr  = errors.New("warp source address is not an owner")
	ErrWrongWarpSourceAddrL = errors.New("warp source address has wrong length")
)

// WarpCredential authorizes an input on behalf of the 20-byte address that
// sent the warp message. The message payload must be an AddressedCall whose
// Payload is sha256(unsigned tx bytes). BLS quorum and source chain are
// verified by the tx executor, not here.
type WarpCredential struct {
	Message []byte `serialize:"true" json:"message"`
}

func (cr *WarpCredential) Verify() error {
	if cr == nil {
		return ErrNilWarpCredential
	}
	return nil
}

// VerifyWarpCredential ensures that the output can be spent by the input with
// the warp credential.
func (fx *Fx) VerifyWarpCredential(utx UnsignedTx, in *Input, cred *WarpCredential, out *OutputOwners) error {
	numSigs := len(in.SigIndices)
	switch {
	case out.Locktime > fx.VM.Clock().Unix():
		return ErrTimelocked
	case out.Threshold < uint32(numSigs):
		return ErrTooManySigners
	case out.Threshold > uint32(numSigs):
		return ErrTooFewSigners
	}

	msg, err := warp.ParseMessage(cred.Message)
	if err != nil {
		return err
	}
	call, err := payload.ParseAddressedCall(msg.Payload)
	if err != nil {
		return err
	}
	if len(call.SourceAddress) != ids.ShortIDLen {
		return fmt.Errorf("%w: %d", ErrWrongWarpSourceAddrL, len(call.SourceAddress))
	}
	if !bytes.Equal(call.Payload, hashing.ComputeHash256(utx.Bytes())) {
		return ErrWrongWarpPayload
	}

	sender := ids.ShortID(call.SourceAddress)
	for _, index := range in.SigIndices {
		if index >= uint32(len(out.Addrs)) {
			return ErrInputOutputIndexOutOfBounds
		}
		if out.Addrs[index] != sender {
			return fmt.Errorf("%w: expected %s but got %s", ErrWrongWarpSourceAddr, out.Addrs[index], sender)
		}
	}
	return nil
}
