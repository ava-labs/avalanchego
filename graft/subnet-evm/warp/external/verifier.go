// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package external

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	ParseErrCode  int32 = 1
	VerifyErrCode int32 = 2

	// SignatureRequestHandlerID is the p2p handler ID for external chain
	// attestation signature requests. It is distinct from
	// p2p.SignatureRequestHandlerID (ACP-118, ID=2), which is reserved for
	// native warp signature requests backed by local chain state. External chain
	// requests require sidecar involvement and are a different protocol.
	SignatureRequestHandlerID = 4
)

var _ acp118.Verifier = (*ExternalChainVerifier)(nil)

// AllowedSources maps source chain type to the set of allowed source program
// or contract addresses on that chain. An empty inner map means all source
// addresses are permitted for that chain type. A missing key means the chain
// type is not permitted at all.
//
// This allowlist is the Go-side equivalent of the address trust decision that
// Nick's method provides automatically on EVM chains. Since non-EVM chains have
// no canonical deployment address mechanism, operators must configure it
// explicitly.
// Q: I think we're gonna need this passed in as a config for the external
// interop
type AllowedSources map[string]map[string]struct{}

// ExternalChainVerifier implements acp118.Verifier for external chain
// attestation messages. It parses the ExternalMessage from the warp payload,
// validates the source against the configured allowlist, then delegates the
// actual chain verification to the local sidecar process.
type ExternalChainVerifier struct {
	sidecar        SidecarClient
	allowedSources AllowedSources
}

// NewExternalChainVerifier constructs an ExternalChainVerifier.
func NewExternalChainVerifier(sidecar SidecarClient, allowedSources AllowedSources) *ExternalChainVerifier {
	return &ExternalChainVerifier{
		sidecar:        sidecar,
		allowedSources: allowedSources,
	}
}

func (v *ExternalChainVerifier) Verify(
	ctx context.Context,
	unsignedMessage *avalancheWarp.UnsignedMessage,
	justification []byte,
) *common.AppError {
	// Parse outer payload as AddressedCall. External messages use an empty
	// SourceAddress since they are not emitted by an on-chain contract.
	parsed, err := payload.Parse(unsignedMessage.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse warp payload: " + err.Error(),
		}
	}
	addressedCall, ok := parsed.(*payload.AddressedCall)
	if !ok {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: fmt.Sprintf("expected AddressedCall payload, got %T", parsed),
		}
	}

	msg, err := ParseExternalMessage(addressedCall.Payload)
	if err != nil {
		return &common.AppError{
			Code:    ParseErrCode,
			Message: "failed to parse ExternalMessage: " + err.Error(),
		}
	}

	if appErr := v.checkAllowlist(msg); appErr != nil {
		return appErr
	}

	if err := v.sidecar.Verify(ctx, &ExternalEvent{
		Message:       msg,
		Justification: justification,
	}); err != nil {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: "sidecar verification failed: " + err.Error(),
		}
	}

	return nil
}

func (v *ExternalChainVerifier) checkAllowlist(msg *ExternalMessage) *common.AppError {
	allowed, ok := v.allowedSources[msg.SourceChainType]
	if !ok {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("source chain type %q is not in the allowlist", msg.SourceChainType),
		}
	}
	// Empty inner map means all source addresses are allowed for this chain type.
	if len(allowed) == 0 {
		return nil
	}
	if _, ok := allowed[msg.SourceAddress]; !ok {
		return &common.AppError{
			Code:    VerifyErrCode,
			Message: fmt.Sprintf("source address %q on chain type %q is not in the allowlist", msg.SourceAddress, msg.SourceChainType),
		}
	}
	return nil
}
