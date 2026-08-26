// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	_ verify.Verifiable = (*WarpCredential)(nil)

	ErrNilWarpCredential   = errors.New("nil warp credential")
	ErrWrongWarpPayload    = errors.New("warp payload is not owner || unsigned tx bytes")
	ErrWrongWarpSourceAddr = errors.New("warp source address is not a trusted helper")
)

// WarpCredential authorizes an input on behalf of a 20-byte owner address.
// The message payload must be an AddressedCall whose Payload is
// owner || unsigned tx bytes, sent by one of Fx.WarpHelpers. BLS quorum and
// source chain are verified by the tx executor, not here.
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
	sender, err := ids.ToShortID(call.SourceAddress)
	if err != nil || !fx.WarpHelpers.Contains(sender) {
		return fmt.Errorf("%w: %x", ErrWrongWarpSourceAddr, call.SourceAddress)
	}
	if len(call.Payload) < ids.ShortIDLen || !bytes.Equal(call.Payload[ids.ShortIDLen:], utx.Bytes()) {
		return ErrWrongWarpPayload
	}
	owner := ids.ShortID(call.Payload[:ids.ShortIDLen])

	for _, index := range in.SigIndices {
		if index >= uint32(len(out.Addrs)) {
			return ErrInputOutputIndexOutOfBounds
		}
		if out.Addrs[index] != owner {
			return fmt.Errorf("%w: expected %s but got %s", ErrWrongWarpSourceAddr, out.Addrs[index], owner)
		}
	}
	return nil
}
