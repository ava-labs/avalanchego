// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	"github.com/ava-labs/avalanchego/vms/evm/warpmsg"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const (
	WarpQuorumNumerator   uint64 = 67
	WarpQuorumDenominator uint64 = 100
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
	errFailedVerification         = errors.New("cannot verify warp signature")
	errCannotRetrieveValidatorSet = errors.New("cannot retrieve validator set")
)

// PredicateGas returns the amount of gas necessary to verify the predicate
// PredicateGas charges for:
// 1. Base cost of the message
// 2. Size of the message
// 3. Number of signers
// 4. TODO: Lookup of the validator set
//
// If the payload of the warp message fails parsing, return a non-nil error invalidating the transaction.
func PredicateGas(pred warpmsg.Packed, rules precompileconfig.Rules) (uint64, error) {
	gasConfig := CurrentGasConfig(rules)

	totalGas := gasConfig.VerifyPredicateBase
	bytesGasCost, overflow := math.SafeMul(gasConfig.PerWarpMessageChunk, uint64(len(pred)))
	if overflow {
		return 0, fmt.Errorf("overflow calculating gas cost for %d warp message chunks", len(pred))
	}
	totalGas, overflow = math.SafeAdd(totalGas, bytesGasCost)
	if overflow {
		return 0, fmt.Errorf("overflow adding gas cost for %d warp message chunks", len(pred))
	}

	unpackedPredicateBytes, err := pred.Unpack()
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
	signerGas, overflow := math.SafeMul(uint64(numSigners), gasConfig.PerWarpSigner)
	if overflow {
		return 0, errOverflowSignersGasCost
	}
	totalGas, overflow = math.SafeAdd(totalGas, signerGas)
	if overflow {
		return 0, fmt.Errorf("overflow adding signer gas (PrevTotal: %d, VerificationGas: %d)", totalGas, signerGas)
	}

	return totalGas, nil
}

// VerifyPredicate verifies the Warp message in pred and its signature.
func VerifyPredicate(predicateContext *precompileconfig.PredicateContext, pred warpmsg.Packed) error {
	unpackedPredicateBytes, err := pred.Unpack()
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}

	// Note: PredicateGas should be called before VerifyPredicate, so we should never reach an error case here.
	warpMsg, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return fmt.Errorf("%w: %w", errCannotParseWarpMsg, err)
	}

	log.Debug("verifying warp message",
		"warpMsg", warpMsg,
		"quorumNum", WarpQuorumNumerator,
		"quorumDenom", WarpQuorumDenominator,
	)

	sourceSubnetID, err := predicateContext.SnowCtx.ValidatorState.GetSubnetID(
		context.TODO(),
		warpMsg.SourceChainID,
	)
	if err != nil {
		log.Debug("failed to retrieve subnetID for chain",
			"msgID", warpMsg.ID(),
			"chainID", warpMsg.SourceChainID,
			"err", err,
		)
		return fmt.Errorf("%w: %w", errCannotRetrieveValidatorSet, err)
	}

	validatorSets, err := predicateContext.SnowCtx.ValidatorState.GetWarpValidatorSets(
		context.TODO(),
		predicateContext.ProposerVMBlockCtx.PChainHeight,
	)
	if err != nil {
		log.Debug("failed to retrieve canonical validator sets",
			"msgID", warpMsg.ID(),
			"subnetID", sourceSubnetID,
			"err", err,
		)
		return fmt.Errorf("%w: %w", errCannotRetrieveValidatorSet, err)
	}
	validatorSet, ok := validatorSets[sourceSubnetID]
	if !ok {
		return fmt.Errorf("%w: %#x source subnet not found", errCannotRetrieveValidatorSet, sourceSubnetID)
	}

	err = warpMsg.Signature.Verify(
		&warpMsg.UnsignedMessage,
		predicateContext.SnowCtx.NetworkID,
		validatorSet,
		WarpQuorumNumerator,
		WarpQuorumDenominator,
	)
	if err != nil {
		log.Debug("failed to verify warp signature",
			"msgID", warpMsg.ID(),
			"err", err,
		)
		return fmt.Errorf("%w: %w", errFailedVerification, err)
	}

	return nil
}
