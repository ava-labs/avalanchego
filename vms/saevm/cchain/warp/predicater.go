// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	evmprecompileconfig "github.com/ava-labs/avalanchego/graft/evm/precompileconfig"
	safemath "github.com/ava-labs/avalanchego/utils/math"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// The C-Chain's quorum: 67 of every 100 units of stake must sign.
const (
	quorumNumerator   uint64 = 67
	quorumDenominator uint64 = 100
)

var (
	errGasOverflow                = errors.New("overflow calculating warp gas")
	errInvalidPredicateBytes      = errors.New("cannot unpack predicate bytes")
	errInvalidWarpMsg             = errors.New("cannot unpack warp message")
	errInvalidWarpMsgPayload      = errors.New("cannot unpack warp message payload")
	errCannotGetNumSigners        = errors.New("cannot fetch num signers from warp message")
	errCannotParseWarpMsg         = errors.New("cannot parse warp message")
	errCannotRetrieveValidatorSet = errors.New("cannot retrieve validator set")
	errFailedVerification         = errors.New("cannot verify warp signature")
)

var _ evmprecompileconfig.Predicater = Predicater{}

// Predicater verifies signed warp messages carried as access-list predicates
// of transactions to the warp precompile.
type Predicater struct{}

// PredicateGas prices verification of pred: a base cost, a cost per 32-byte
// chunk, and a cost per signer. An unparseable predicate is an error, which
// invalidates the transaction.
func (Predicater) PredicateGas(pred predicate.Predicate, _ evmprecompileconfig.Rules) (uint64, error) {
	chunkGas, err := safemath.Mul(Gas.PerWarpMessageChunk, uint64(len(pred)))
	if err != nil {
		return 0, fmt.Errorf("%w: %d chunks: %w", errGasOverflow, len(pred), err)
	}
	total, err := safemath.Add(Gas.VerifyPredicateBase, chunkGas)
	if err != nil {
		return 0, fmt.Errorf("%w: adding chunk gas: %w", errGasOverflow, err)
	}

	b, err := pred.Bytes()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}
	msg, err := avalanchewarp.ParseMessage(b)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidWarpMsg, err)
	}
	if _, err := payload.Parse(msg.Payload); err != nil {
		return 0, fmt.Errorf("%w: %w", errInvalidWarpMsgPayload, err)
	}
	numSigners, err := msg.Signature.NumSigners()
	if err != nil {
		return 0, fmt.Errorf("%w: %w", errCannotGetNumSigners, err)
	}
	signerGas, err := safemath.Mul(uint64(numSigners), Gas.PerWarpSigner) //#nosec G115 -- NumSigners is a non-negative count
	if err != nil {
		return 0, fmt.Errorf("%w: %d signers: %w", errGasOverflow, numSigners, err)
	}
	total, err = safemath.Add(total, signerGas)
	if err != nil {
		return 0, fmt.Errorf("%w: adding signer gas: %w", errGasOverflow, err)
	}
	return total, nil
}

// VerifyPredicate checks that pred carries a warp message signed by a quorum
// of the source chain's validators at the P-chain height in pc. Messages
// from primary-network chains are verified against this chain's own
// validator set, as coreth did for the C-Chain.
func (Predicater) VerifyPredicate(pc *evmprecompileconfig.PredicateContext, pred predicate.Predicate) error {
	b, err := pred.Bytes()
	if err != nil {
		return fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}
	msg, err := avalanchewarp.ParseMessage(b)
	if err != nil {
		return fmt.Errorf("%w: %w", errCannotParseWarpMsg, err)
	}

	// The Predicater interface carries no context; verification is
	// synchronous and bounded by the validator-set lookup.
	ctx := context.TODO()
	sourceSubnetID, err := pc.SnowCtx.ValidatorState.GetSubnetID(ctx, msg.SourceChainID)
	if err != nil {
		return fmt.Errorf("%w: subnet of chain %s: %w", errCannotRetrieveValidatorSet, msg.SourceChainID, err)
	}
	if sourceSubnetID == constants.PrimaryNetworkID {
		sourceSubnetID = pc.SnowCtx.SubnetID
	}

	validatorSets, err := pc.SnowCtx.ValidatorState.GetWarpValidatorSets(ctx, pc.ProposerVMBlockCtx.PChainHeight)
	if err != nil {
		return fmt.Errorf("%w: at height %d: %w", errCannotRetrieveValidatorSet, pc.ProposerVMBlockCtx.PChainHeight, err)
	}
	validatorSet, ok := validatorSets[sourceSubnetID]
	if !ok {
		return fmt.Errorf("%w: %s source subnet not found", errCannotRetrieveValidatorSet, sourceSubnetID)
	}

	if err := msg.Signature.Verify(
		&msg.UnsignedMessage,
		pc.SnowCtx.NetworkID,
		validatorSet,
		quorumNumerator,
		quorumDenominator,
	); err != nil {
		return fmt.Errorf("%w: %w", errFailedVerification, err)
	}
	return nil
}
