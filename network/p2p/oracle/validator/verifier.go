// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/p2p/oracle"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	// SignatureRequestHandlerID is the p2p handler ID for oracle attestation
	// requests. Distinct from acp118.HandlerID (2) which is reserved for
	// native warp. Oracle attestations require sidecar involvement and are a
	// separate protocol.
	SignatureRequestHandlerID uint64 = p2p.OracleSignatureRequestHandlerID

	errCodeParse  int32 = 1
	errCodeVerify int32 = 2
)

// OracleVerifier implements acp118.Verifier for oracle attestation messages.
// It parses the OracleMessage from the warp payload, rejects source types the
// local sidecar has not declared support for, and otherwise delegates
// verification to a SidecarClient.
//
// The allowed-source-type set exists so that a node can fast-reject signature
// requests for chains its sidecar cannot handle (e.g. a Bitcoin request
// arriving at a Solana-only node), before doing any RPC work. The set is
// derived from the sidecar's own config file — see sidecar/config and the
// wiring in graft/subnet-evm/plugin/evm/vm.go.
type OracleVerifier struct {
	sidecar        oracle.SidecarClient
	allowedSources map[string]struct{}
}

var _ acp118.Verifier = (*OracleVerifier)(nil)

// NewOracleVerifier constructs an OracleVerifier that will reject any message
// whose SourceType is not in allowed. A nil or empty allowed set rejects all
// messages — callers must pass at least one source type to enable verification.
func NewOracleVerifier(sidecar oracle.SidecarClient, allowed map[string]struct{}) *OracleVerifier {
	return &OracleVerifier{sidecar: sidecar, allowedSources: allowed}
}

func (v *OracleVerifier) Verify(
	ctx context.Context,
	unsignedMessage *warp.UnsignedMessage,
	justification []byte,
) *common.AppError {
	ac, err := payload.ParseAddressedCall(unsignedMessage.Payload)
	if err != nil {
		return &common.AppError{
			Code:    errCodeParse,
			Message: "failed to parse AddressedCall: " + err.Error(),
		}
	}
	msg, err := oracle.ParseOracleMessage(ac.Payload)
	if err != nil {
		return &common.AppError{
			Code:    errCodeParse,
			Message: "failed to parse OracleMessage: " + err.Error(),
		}
	}

	if _, ok := v.allowedSources[msg.SourceType]; !ok {
		return &common.AppError{
			Code:    errCodeVerify,
			Message: "source type not supported by this node: " + msg.SourceType,
		}
	}

	if err := v.sidecar.Verify(ctx, &oracle.OracleEvent{
		Message:       msg,
		Justification: justification,
	}); err != nil {
		return &common.AppError{
			Code:    errCodeVerify,
			Message: "sidecar verification failed: " + err.Error(),
		}
	}

	return nil
}
