// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package teleporter provides the attestor-gateway verifier for native
// TeleporterMessageV2 signing (Phase C step 1b). Unlike the OracleMessage-format
// verifier, the payload this signs is abi.encode(TeleporterMessageV2) — the exact
// bytes stock TeleporterMessengerV2 + WarpAdapter expect — so the Teleporter
// content itself carries no lookup fields. Instead the relayer supplies those in
// the justification, and the verifier reconstructs an OracleMessage from them and
// delegates the actual chain check to the unchanged sidecar.
package teleporter

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	SignatureRequestHandlerID uint64 = p2p.TeleporterSignatureRequestHandlerID

	errCodeParse  int32 = 1
	errCodeVerify int32 = 2

	// justificationLen is the fixed layout the relayer sends: the source-chain
	// tx hash (32) + the outbox contract address (20) + the block height (8).
	// The Teleporter payload has no such fields, so they travel here.
	justificationLen = 32 + 20 + 8
)

// Justification is the relayer-supplied lookup metadata for a Teleporter-format
// attestation. It is NOT part of the signed output.
type Justification struct {
	TxHash      [32]byte
	Outbox      common.Address
	BlockHeight uint64
}

func (j Justification) Encode() []byte {
	out := make([]byte, justificationLen)
	copy(out[0:32], j.TxHash[:])
	copy(out[32:52], j.Outbox.Bytes())
	binary.BigEndian.PutUint64(out[52:60], j.BlockHeight)
	return out
}

func ParseJustification(b []byte) (Justification, error) {
	var j Justification
	if len(b) != justificationLen {
		return j, fmt.Errorf("justification must be %d bytes, got %d", justificationLen, len(b))
	}
	copy(j.TxHash[:], b[0:32])
	j.Outbox = common.BytesToAddress(b[32:52])
	j.BlockHeight = binary.BigEndian.Uint64(b[52:60])
	return j, nil
}

// TeleporterVerifier reconstructs an OracleMessage from the Teleporter-format
// warp message plus the relayer justification, then delegates to the sidecar.
// The sidecar and its evmrpc verifier are unchanged: they still perform the
// byte-exact "did the outbox emit these bytes at this height" check.
type TeleporterVerifier struct {
	sidecar oracle.SidecarClient
}

var _ acp118.Verifier = (*TeleporterVerifier)(nil)

func NewTeleporterVerifier(sidecar oracle.SidecarClient) *TeleporterVerifier {
	return &TeleporterVerifier{sidecar: sidecar}
}

func (v *TeleporterVerifier) Verify(
	ctx context.Context,
	unsignedMessage *warp.UnsignedMessage,
	justification []byte,
) *commonEng.AppError {
	ac, err := payload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		return &commonEng.AppError{Code: errCodeParse, Message: "failed to parse AddressedCall: " + err.Error()}
	}
	// ac.Payload is abi.encode(TeleporterMessageV2) — the exact bytes the outbox
	// must have emitted. We do not decode it here; we verify the outbox emitted
	// it verbatim.
	j, err := ParseJustification(justification)
	if err != nil {
		return &commonEng.AppError{Code: errCodeParse, Message: "failed to parse justification: " + err.Error()}
	}

	// Reconstruct an EVM OracleMessage: check that the outbox contract emitted a
	// log whose data equals the Teleporter payload, in the claimed tx/block.
	oracleMsg, err := oracle.NewOracleMessage(
		oracle.SourceTypeEVM,
		j.Outbox.Hex(),
		common.Address{}, // destContract unused for verification
		j.BlockHeight,
		0, // nonce unused for verification
		ac.Payload,
	)
	if err != nil {
		return &commonEng.AppError{Code: errCodeParse, Message: "failed to build OracleMessage: " + err.Error()}
	}

	// The sidecar's evmrpc verifier takes the raw tx hash as its justification.
	if err := v.sidecar.Verify(ctx, &oracle.OracleEvent{
		Message:       oracleMsg,
		Justification: j.TxHash[:],
	}); err != nil {
		return &commonEng.AppError{Code: errCodeVerify, Message: "sidecar verification failed: " + err.Error()}
	}
	return nil
}
