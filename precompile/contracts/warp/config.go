// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/precompileconfig"
	"github.com/ava-labs/coreth/predicate"
	warpValidators "github.com/ava-labs/coreth/warp/validators"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/log"
)

var (
	_ precompileconfig.Config     = &Config{}
	_ precompileconfig.Predicater = &Config{}
	_ precompileconfig.Accepter   = &Config{}
)

var (
	errOverflowSignersGasCost  = errors.New("overflow calculating warp signers gas cost")
	errInvalidPredicateBytes   = errors.New("cannot unpack predicate bytes")
	errInvalidWarpMsg          = errors.New("cannot unpack warp message")
	errInvalidWarpMsgPayload   = errors.New("cannot unpack warp message payload")
	errInvalidAddressedPayload = errors.New("cannot unpack addressed payload")
	errInvalidBlockHashPayload = errors.New("cannot unpack block hash payload")
	errCannotGetNumSigners     = errors.New("cannot fetch num signers from warp message")
	errWarpCannotBeActivated   = errors.New("warp cannot be activated before DUpgrade")
)

// Config implements the precompileconfig.Config interface and
// adds specific configuration for Warp.
type Config struct {
	precompileconfig.Upgrade
	QuorumNumerator uint64 `json:"quorumNumerator"`
}

// NewConfig returns a config for a network upgrade at [blockTimestamp] that enables
// Warp with the given quorum numerator.
func NewConfig(blockTimestamp *uint64, quorumNumerator uint64) *Config {
	return &Config{
		Upgrade:         precompileconfig.Upgrade{BlockTimestamp: blockTimestamp},
		QuorumNumerator: quorumNumerator,
	}
}

// NewDefaultConfig returns a config for a network upgrade at [blockTimestamp] that enables
// Warp with the default quorum numerator (0 denotes using the default).
func NewDefaultConfig(blockTimestamp *uint64) *Config {
	return NewConfig(blockTimestamp, 0)
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
		// If Warp attempts to activate before the DUpgrade, fail verification
		timestamp := *c.Timestamp()
		if !chainConfig.IsDUpgrade(timestamp) {
			return errWarpCannotBeActivated
		}
	}

	if c.QuorumNumerator > params.WarpQuorumDenominator {
		return fmt.Errorf("cannot specify quorum numerator (%d) > quorum denominator (%d)", c.QuorumNumerator, params.WarpQuorumDenominator)
	}
	// If a non-default quorum numerator is specified and it is less than the minimum, return an error
	if c.QuorumNumerator != 0 && c.QuorumNumerator < params.WarpQuorumNumeratorMinimum {
		return fmt.Errorf("cannot specify quorum numerator (%d) < min quorum numerator (%d)", c.QuorumNumerator, params.WarpQuorumNumeratorMinimum)
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

func (c *Config) Accept(acceptCtx *precompileconfig.AcceptContext, blockHash common.Hash, blockNumber uint64, txHash common.Hash, logIndex int, topics []common.Hash, logData []byte) error {
	unsignedMessage, err := UnpackSendWarpEventDataToMessage(logData)
	if err != nil {
		return fmt.Errorf("failed to parse warp log data into unsigned message (TxHash: %s, LogIndex: %d): %w", txHash, logIndex, err)
	}
	log.Info(
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

// verifyWarpMessage checks that [warpMsg] can be parsed as an addressed payload and verifies the Warp Message Signature
// within [predicateContext].
func (c *Config) verifyWarpMessage(predicateContext *precompileconfig.PredicateContext, warpMsg *warp.Message) bool {
	// Use default quorum numerator unless config specifies a non-default option
	quorumNumerator := params.WarpDefaultQuorumNumerator
	if c.QuorumNumerator != 0 {
		quorumNumerator = c.QuorumNumerator
	}

	log.Debug("verifying warp message", "warpMsg", warpMsg, "quorumNum", quorumNumerator, "quorumDenom", params.WarpQuorumDenominator)
	if err := warpMsg.Signature.Verify(
		context.Background(),
		&warpMsg.UnsignedMessage,
		predicateContext.SnowCtx.NetworkID,
		warpValidators.NewState(predicateContext.SnowCtx), // Wrap validators.State on the chain snow context to special case the Primary Network
		predicateContext.ProposerVMBlockCtx.PChainHeight,
		quorumNumerator,
		params.WarpQuorumDenominator,
	); err != nil {
		log.Debug("failed to verify warp signature", "msgID", warpMsg.ID(), "err", err)
		return false
	}

	return true
}

// PredicateGas returns the amount of gas necessary to verify the predicate
// PredicateGas charges for:
// 1. Base cost of the message
// 2. Size of the message
// 3. Number of signers
// 4. TODO: Lookup of the validator set
//
// If the payload of the warp message fails parsing, return a non-nil error invalidating the transaction.
func (c *Config) PredicateGas(predicateBytes []byte) (uint64, error) {
	totalGas := GasCostPerSignatureVerification
	bytesGasCost, overflow := math.SafeMul(GasCostPerWarpMessageBytes, uint64(len(predicateBytes)))
	if overflow {
		return 0, fmt.Errorf("overflow calculating gas cost for warp message bytes of size %d", len(predicateBytes))
	}
	totalGas, overflow = math.SafeAdd(totalGas, bytesGasCost)
	if overflow {
		return 0, fmt.Errorf("overflow adding bytes gas cost of size %d", len(predicateBytes))
	}

	unpackedPredicateBytes, err := predicate.UnpackPredicate(predicateBytes)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", errInvalidPredicateBytes, err)
	}
	warpMessage, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", errInvalidWarpMsg, err)
	}
	_, err = payload.Parse(warpMessage.Payload)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", errInvalidWarpMsgPayload, err)
	}

	numSigners, err := warpMessage.Signature.NumSigners()
	if err != nil {
		return 0, fmt.Errorf("%w: %s", errCannotGetNumSigners, err)
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

func (c *Config) verifyPredicate(predicateContext *precompileconfig.PredicateContext, predicateBytes []byte) bool {
	unpackedPredicateBytes, err := predicate.UnpackPredicate(predicateBytes)
	if err != nil {
		return false
	}

	// Note: PredicateGas should be called before VerifyPredicate, so we should never reach an error case here.
	warpMessage, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return false
	}
	return c.verifyWarpMessage(predicateContext, warpMessage)
}

// VerifyPredicate computes indices of predicates that failed verification as a bitset then returns the result
// as a byte slice.
func (c *Config) VerifyPredicate(predicateContext *precompileconfig.PredicateContext, predicates [][]byte) []byte {
	resultBitSet := set.NewBits()

	for predicateIndex, predicateBytes := range predicates {
		if !c.verifyPredicate(predicateContext, predicateBytes) {
			resultBitSet.Add(predicateIndex)
		}
	}
	return resultBitSet.Bytes()
}
