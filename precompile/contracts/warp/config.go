// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/precompile/precompileconfig"

	warpValidators "github.com/ava-labs/coreth/warp/validators"
)

const (
	WarpDefaultQuorumNumerator uint64 = 67
	WarpQuorumNumeratorMinimum uint64 = 33
	WarpQuorumDenominator      uint64 = 100
)

var (
	_ precompileconfig.Config     = (*Config)(nil)
	_ precompileconfig.Predicater = (*Config)(nil)
	_ precompileconfig.Accepter   = (*Config)(nil)
)

var (
	errOverflowSignersGasCost     = errors.New("overflow calculating warp signers gas cost")
	errInvalidPredicateBytes      = errors.New("cannot unpack predicate bytes")
	errInvalidWarpMsg             = errors.New("cannot unpack warp message")
	errCannotParseWarpMsg         = errors.New("cannot parse warp message")
	errInvalidWarpMsgPayload      = errors.New("cannot unpack warp message payload")
	errInvalidAddressedPayload    = errors.New("cannot unpack addressed payload")
	errInvalidBlockHashPayload    = errors.New("cannot unpack block hash payload")
	errCannotGetNumSigners        = errors.New("cannot fetch num signers from warp message")
	errWarpCannotBeActivated      = errors.New("warp cannot be activated before Durango")
	errFailedVerification         = errors.New("cannot verify warp signature")
	errCannotRetrieveValidatorSet = errors.New("cannot retrieve validator set")
)

// Config implements the precompileconfig.Config interface and
// adds specific configuration for Warp.
type Config struct {
	precompileconfig.Upgrade
	QuorumNumerator              uint64 `json:"quorumNumerator"`
	RequirePrimaryNetworkSigners bool   `json:"requirePrimaryNetworkSigners"`
}

// NewConfig returns a config for a network upgrade at [blockTimestamp] that enables
// Warp with the given quorum numerator.
func NewConfig(blockTimestamp *uint64, quorumNumerator uint64, requirePrimaryNetworkSigners bool) *Config {
	return &Config{
		Upgrade:                      precompileconfig.Upgrade{BlockTimestamp: blockTimestamp},
		QuorumNumerator:              quorumNumerator,
		RequirePrimaryNetworkSigners: requirePrimaryNetworkSigners,
	}
}

// NewDefaultConfig returns a config for a network upgrade at [blockTimestamp] that enables
// Warp with the default quorum numerator (0 denotes using the default).
func NewDefaultConfig(blockTimestamp *uint64) *Config {
	return NewConfig(blockTimestamp, 0, false)
}

// NewDisableConfig returns config for a network upgrade at [blockTimestamp]
// that disables Warp.
func NewDisableConfig(blockTimestamp *uint64) *Config {
	return &Config{
		Upgrade: precompileconfig.Upgrade{
			BlockTimestamp: blockTimestamp,
			Disable:        true,
		},
	}
}

// Key returns the key for the Warp precompileconfig.
// This should be the same key as used in the precompile module.
func (*Config) Key() string { return ConfigKey }

// Verify tries to verify Config and returns an error accordingly.
func (c *Config) Verify(chainConfig precompileconfig.ChainConfig) error {
	if c.Timestamp() != nil {
		// If Warp attempts to activate before Durango, fail verification
		timestamp := *c.Timestamp()
		if !chainConfig.IsDurango(timestamp) {
			return errWarpCannotBeActivated
		}
	}

	if c.QuorumNumerator > WarpQuorumDenominator {
		return fmt.Errorf("cannot specify quorum numerator (%d) > quorum denominator (%d)", c.QuorumNumerator, WarpQuorumDenominator)
	}
	// If a non-default quorum numerator is specified and it is less than the minimum, return an error
	if c.QuorumNumerator != 0 && c.QuorumNumerator < WarpQuorumNumeratorMinimum {
		return fmt.Errorf("cannot specify quorum numerator (%d) < min quorum numerator (%d)", c.QuorumNumerator, WarpQuorumNumeratorMinimum)
	}
	return nil
}

// Equal returns true if [s] is a [*Config] and it has been configured identical to [c].
func (c *Config) Equal(s precompileconfig.Config) bool {
	// typecast before comparison
	other, ok := (s).(*Config)
	if !ok {
		return false
	}
	equals := c.Upgrade.Equal(&other.Upgrade)
	return equals && c.QuorumNumerator == other.QuorumNumerator
}

func (*Config) Accept(acceptCtx *precompileconfig.AcceptContext, blockHash common.Hash, blockNumber uint64, txHash common.Hash, logIndex int, _ []common.Hash, logData []byte) error {
	unsignedMessage, err := UnpackSendWarpEventDataToMessage(logData)
	if err != nil {
		return fmt.Errorf("failed to parse warp log data into unsigned message (TxHash: %s, LogIndex: %d): %w", txHash, logIndex, err)
	}
	log.Debug(
		"Accepted warp unsigned message",
		"blockHash", blockHash,
		"blockNumber", blockNumber,
		"txHash", txHash,
		"logIndex", logIndex,
		"logData", common.Bytes2Hex(logData),
		"warpMessageID", unsignedMessage.ID(),
	)
	if err := acceptCtx.Warp.AddMessage(unsignedMessage); err != nil {
		return fmt.Errorf("failed to add warp message during accept (TxHash: %s, LogIndex: %d): %w", txHash, logIndex, err)
	}
	return nil
}

// PredicateGas returns the amount of gas necessary to verify the predicate
// PredicateGas charges for:
// 1. Base cost of the message
// 2. Size of the message
// 3. Number of signers
// 4. TODO: Lookup of the validator set
//
// If the payload of the warp message fails parsing, return a non-nil error invalidating the transaction.
func (*Config) PredicateGas(pred predicate.Predicate) (uint64, error) {
	totalGas := GasCostPerSignatureVerification
	bytesGasCost, overflow := math.SafeMul(GasCostPerWarpMessageChunk, uint64(len(pred)))
	if overflow {
		return 0, fmt.Errorf("overflow calculating gas cost for %d warp message chunks", len(pred))
	}
	totalGas, overflow = math.SafeAdd(totalGas, bytesGasCost)
	if overflow {
		return 0, fmt.Errorf("overflow adding gas cost for %d warp message chunks", len(pred))
	}

	unpackedPredicateBytes, err := pred.Bytes()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}
	warpMessage, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidWarpMsg, err)
	}
	_, err = payload.Parse(warpMessage.Payload)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidWarpMsgPayload, err)
	}

	numSigners, err := warpMessage.Signature.NumSigners()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errCannotGetNumSigners, err)
	}
	signerGas, overflow := math.SafeMul(uint64(numSigners), GasCostPerWarpSigner)
	if overflow {
		return 0, errOverflowSignersGasCost
	}
	totalGas, overflow = math.SafeAdd(totalGas, signerGas)
	if overflow {
		return 0, fmt.Errorf("overflow adding signer gas (PrevTotal: %d, VerificationGas: %d)", totalGas, signerGas)
	}

	return totalGas, nil
}

// VerifyPredicate returns whether the predicate described by [predicateBytes] passes verification.
func (c *Config) VerifyPredicate(predicateContext *precompileconfig.PredicateContext, pred predicate.Predicate) error {
	unpackedPredicateBytes, err := pred.Bytes()
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}

	// Note: PredicateGas should be called before VerifyPredicate, so we should never reach an error case here.
	warpMsg, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return fmt.Errorf("%w: %w", errCannotParseWarpMsg, err)
	}

	quorumNumerator := WarpDefaultQuorumNumerator
	if c.QuorumNumerator != 0 {
		quorumNumerator = c.QuorumNumerator
	}

	log.Debug("verifying warp message", "warpMsg", warpMsg, "quorumNum", quorumNumerator, "quorumDenom", WarpQuorumDenominator)

	// Wrap validators.State on the chain snow context to special case the Primary Network
	state := warpValidators.NewState(
		predicateContext.SnowCtx.ValidatorState,
		predicateContext.SnowCtx.SubnetID,
		warpMsg.SourceChainID,
		c.RequirePrimaryNetworkSigners,
	)

	validatorSet, err := warp.GetCanonicalValidatorSetFromChainID(
		context.Background(),
		state,
		predicateContext.ProposerVMBlockCtx.PChainHeight,
		warpMsg.UnsignedMessage.SourceChainID,
	)
	if err != nil {
		log.Debug("failed to retrieve canonical validator set", "msgID", warpMsg.ID(), "err", err)
		return fmt.Errorf("%w: %w", errCannotRetrieveValidatorSet, err)
	}

	err = warpMsg.Signature.Verify(
		&warpMsg.UnsignedMessage,
		predicateContext.SnowCtx.NetworkID,
		validatorSet,
		quorumNumerator,
		WarpQuorumDenominator,
	)
	if err != nil {
		log.Debug("failed to verify warp signature", "msgID", warpMsg.ID(), "err", err)
		return fmt.Errorf("%w: %w", errFailedVerification, err)
	}

	return nil
}
