// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"context"
	"fmt"

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

// AllowedSources maps source type to the set of allowed source addresses
// on that source. An empty inner map means all source addresses are permitted
// for that source type. A missing key means the source type is not permitted.
type AllowedSources map[string]map[string]struct{}

// OracleVerifier implements acp118.Verifier for oracle attestation messages.
// It parses the OracleMessage from the warp payload, validates the source
// against the configured allowlist, then delegates the actual verification to
// a SidecarClient.
type OracleVerifier struct {
	sidecar        oracle.SidecarClient
	allowedSources AllowedSources
}

var _ acp118.Verifier = (*OracleVerifier)(nil)

func NewOracleVerifier(sidecar oracle.SidecarClient, allowedSources AllowedSources) *OracleVerifier {
	return &OracleVerifier{
		sidecar:        sidecar,
		allowedSources: allowedSources,
	}
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

	if appErr := v.checkAllowlist(msg); appErr != nil {
		return appErr
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

func (v *OracleVerifier) checkAllowlist(msg *oracle.OracleMessage) *common.AppError {
	allowed, ok := v.allowedSources[msg.SourceType]
	if !ok {
		return &common.AppError{
			Code:    errCodeVerify,
			Message: fmt.Sprintf("source type %q is not in the allowlist", msg.SourceType),
		}
	}
	// Empty inner map means all source addresses are allowed for this source type.
	if len(allowed) == 0 {
		return nil
	}
	if _, ok := allowed[msg.SourceAddress]; !ok {
		return &common.AppError{
			Code:    errCodeVerify,
			Message: fmt.Sprintf("source address %q on source type %q is not in the allowlist", msg.SourceAddress, msg.SourceType),
		}
	}
	return nil
}
