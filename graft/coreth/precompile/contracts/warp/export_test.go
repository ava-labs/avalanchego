// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

// Exported for the external warp_test package.
var (
	ErrCannotGetNumSigners     = errCannotGetNumSigners
	ErrFailedVerification      = errFailedVerification
	ErrInvalidAddressedPayload = errInvalidAddressedPayload
	ErrInvalidBlockHashPayload = errInvalidBlockHashPayload
	ErrInvalidIndexInput       = errInvalidIndexInput
	ErrInvalidPredicateBytes   = errInvalidPredicateBytes
	ErrInvalidSendInput        = errInvalidSendInput
	ErrInvalidWarpMsg          = errInvalidWarpMsg
	ErrInvalidWarpMsgPayload   = errInvalidWarpMsgPayload

	GraniteGasConfig = graniteGasConfig
)

func (g GasConfig) GetVerifiedWarpMessageCost(chunks int) uint64 {
	return g.GetVerifiedWarpMessageBase + uint64(chunks)*g.PerWarpMessageChunk
}

func (g GasConfig) SendWarpMessageCost(bytes int) uint64 {
	return g.SendWarpMessageBase + uint64(bytes)*g.PerWarpMessageByte
}

func (g GasConfig) PredicateGasCost(chunks int, signers int) uint64 {
	return g.VerifyPredicateBase + uint64(chunks)*g.PerWarpMessageChunk + uint64(signers)*g.PerWarpSigner
}
