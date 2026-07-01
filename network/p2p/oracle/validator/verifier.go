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
// It parses the OracleMessage from the warp payload and delegates verification
// to a SidecarClient. All source-level allowlisting is the sidecar's responsibility.
type OracleVerifier struct {
	sidecar oracle.SidecarClient
}

var _ acp118.Verifier = (*OracleVerifier)(nil)

func NewOracleVerifier(sidecar oracle.SidecarClient) *OracleVerifier {
	return &OracleVerifier{sidecar: sidecar}
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
