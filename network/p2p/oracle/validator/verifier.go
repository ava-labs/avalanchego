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
	// Distinct from acp118.HandlerID (2, native warp) so oracle attestations
	// can evolve independently of the warp signing protocol.
	SignatureRequestHandlerID uint64 = p2p.OracleSignatureRequestHandlerID

	errCodeParse  int32 = 1
	errCodeVerify int32 = 2
)

// OracleVerifier fast-rejects unsupported source types then delegates to the sidecar.
// allowedSources is derived from the sidecar config; a nil/empty set rejects everything.
type OracleVerifier struct {
	sidecar        oracle.SidecarClient
	allowedSources map[string]struct{}
}

var _ acp118.Verifier = (*OracleVerifier)(nil)

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
