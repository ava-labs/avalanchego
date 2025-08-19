// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/log"

	warpprecompile "github.com/ava-labs/subnet-evm/precompile/contracts/warp"
	warpValidators "github.com/ava-labs/subnet-evm/warp/validators"
)

var errNoValidators = errors.New("cannot aggregate signatures from subnet with no validators")

// API introduces snowman specific functionality to the evm
type API struct {
	chainContext                 *snow.Context
	backend                      Backend
	signatureAggregator          *acp118.SignatureAggregator
	requirePrimaryNetworkSigners func() bool
}

func NewAPI(chainCtx *snow.Context, backend Backend, signatureAggregator *acp118.SignatureAggregator, requirePrimaryNetworkSigners func() bool) *API {
	return &API{
		backend:                      backend,
		chainContext:                 chainCtx,
		signatureAggregator:          signatureAggregator,
		requirePrimaryNetworkSigners: requirePrimaryNetworkSigners,
	}
}

// GetMessage returns the Warp message associated with a messageID.
func (a *API) GetMessage(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	message, err := a.backend.GetMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s with error %w", messageID, err)
	}
	return hexutil.Bytes(message.Bytes()), nil
}

// GetMessageSignature returns the BLS signature associated with a messageID.
func (a *API) GetMessageSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	unsignedMessage, err := a.backend.GetMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s with error %w", messageID, err)
	}
	signature, err := a.backend.GetMessageSignature(ctx, unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to get signature for message %s with error %w", messageID, err)
	}
	return signature[:], nil
}

// GetBlockSignature returns the BLS signature associated with a blockID.
func (a *API) GetBlockSignature(ctx context.Context, blockID ids.ID) (hexutil.Bytes, error) {
	signature, err := a.backend.GetBlockSignature(ctx, blockID)
	if err != nil {
		return nil, fmt.Errorf("failed to get signature for block %s with error %w", blockID, err)
	}
	return signature[:], nil
}

// GetMessageAggregateSignature fetches the aggregate signature for the requested [messageID]
func (a *API) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetIDStr string) (signedMessageBytes hexutil.Bytes, err error) {
	unsignedMessage, err := a.backend.GetMessage(messageID)
	if err != nil {
		return nil, err
	}
	return a.aggregateSignatures(ctx, unsignedMessage, quorumNum, subnetIDStr)
}

// GetBlockAggregateSignature fetches the aggregate signature for the requested [blockID]
func (a *API) GetBlockAggregateSignature(ctx context.Context, blockID ids.ID, quorumNum uint64, subnetIDStr string) (signedMessageBytes hexutil.Bytes, err error) {
	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return nil, err
	}
	unsignedMessage, err := warp.NewUnsignedMessage(a.chainContext.NetworkID, a.chainContext.ChainID, blockHashPayload.Bytes())
	if err != nil {
		return nil, err
	}

	return a.aggregateSignatures(ctx, unsignedMessage, quorumNum, subnetIDStr)
}

func (a *API) aggregateSignatures(ctx context.Context, unsignedMessage *warp.UnsignedMessage, quorumNum uint64, subnetIDStr string) (hexutil.Bytes, error) {
	subnetID := a.chainContext.SubnetID
	if len(subnetIDStr) > 0 {
		sid, err := ids.FromString(subnetIDStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse subnetID: %q", subnetIDStr)
		}
		subnetID = sid
	}
	validatorState := a.chainContext.ValidatorState
	pChainHeight, err := validatorState.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}

	state := warpValidators.NewState(validatorState, a.chainContext.SubnetID, a.chainContext.ChainID, a.requirePrimaryNetworkSigners())
	validatorSet, err := warp.GetCanonicalValidatorSetFromSubnetID(ctx, state, pChainHeight, subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set: %w", err)
	}
	if len(validatorSet.Validators) == 0 {
		return nil, fmt.Errorf("%w (SubnetID: %s, Height: %d)", errNoValidators, subnetID, pChainHeight)
	}

	log.Debug("Fetching signature",
		"sourceSubnetID", subnetID,
		"height", pChainHeight,
		"numValidators", len(validatorSet.Validators),
		"totalWeight", validatorSet.TotalWeight,
	)
	warpMessage := &warp.Message{
		UnsignedMessage: *unsignedMessage,
		Signature:       &warp.BitSetSignature{},
	}
	signedMessage, _, _, err := a.signatureAggregator.AggregateSignatures(
		ctx,
		warpMessage,
		nil,
		validatorSet.Validators,
		quorumNum,
		warpprecompile.WarpQuorumDenominator,
	)
	if err != nil {
		return nil, err
	}
	// TODO: return the signature and total weight as well to the caller for more complete details
	// Need to decide on the best UI for this and write up documentation with the potential
	// gotchas that could impact signed messages becoming invalid.
	return hexutil.Bytes(signedMessage.Bytes()), nil
}
