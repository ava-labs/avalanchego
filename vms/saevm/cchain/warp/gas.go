// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import ethparams "github.com/ava-labs/libevm/params"

// Gas costs the warp precompile charges. Only the Granite schedule exists
// here: the precompile is served only under Helicon rules, which imply
// Granite, because coreth's rules hooks honour the SAE precompile set only
// when IsHelicon. Pre-Helicon blocks (e.g. replayed through debug RPCs) use
// coreth's warp precompile and gas schedule instead. The values MUST match
// coreth's graniteGasConfig.
type GasConfig struct {
	// Cost to call getBlockchainID.
	GetBlockchainID uint64
	// Base cost of entering getVerifiedWarpMessage or getVerifiedWarpBlockHash.
	GetVerifiedWarpMessageBase uint64
	// Cost per signer in the message's validator set.
	PerWarpSigner uint64
	// Cost per 32-byte chunk of the message.
	PerWarpMessageChunk uint64
	// Base cost of verifying a predicate.
	VerifyPredicateBase uint64
	// Base cost of entering sendWarpMessage.
	SendWarpMessageBase uint64
	// Cost per byte of sendWarpMessage input.
	PerWarpMessageByte uint64
}

const (
	// addWarpMessageBaseGasCost covers producing and serving a BLS signature.
	addWarpMessageBaseGasCost uint64 = 20_000
	// writeGasCostPerSlot is the trie write cost coreth charges for persisting
	// the message, a conservative overestimate kept for compatibility.
	writeGasCostPerSlot uint64 = 20_000
)

// Gas is the Granite gas schedule.
var Gas = GasConfig{
	GetBlockchainID:            200,
	GetVerifiedWarpMessageBase: 750,
	PerWarpSigner:              250,
	PerWarpMessageChunk:        512,
	VerifyPredicateBase:        125_000,
	// Base log gas, three topics, and producing + serving a BLS signature.
	SendWarpMessageBase: ethparams.LogGas + 3*ethparams.LogTopicGas + addWarpMessageBaseGasCost + writeGasCostPerSlot,
	PerWarpMessageByte:  ethparams.LogDataGas,
}
