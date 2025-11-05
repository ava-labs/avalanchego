// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var errNoValidators = errors.New("cannot aggregate signatures from subnet with no validators")

// API introduces snowman specific functionality to the evm
type API struct {
	chainContext        *snow.Context
	db                  *DB
	signer              *Signer
	blockClient         BlockStore
	signatureAggregator *acp118.SignatureAggregator
}

func NewAPI(chainCtx *snow.Context, db *DB, signer *Signer, blockClient BlockStore, signatureAggregator *acp118.SignatureAggregator) *API {
	return &API{
		db:                  db,
		signer:              signer,
		blockClient:         blockClient,
		chainContext:        chainCtx,
		signatureAggregator: signatureAggregator,
	}
}

// GetMessage returns the Warp message associated with a messageID.
func (a *API) GetMessage(_ context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	message, err := a.db.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s with error %w", messageID, err)
	}
	return hexutil.Bytes(message.Bytes()), nil
}

// GetMessageSignature returns the BLS signature associated with a messageID.
func (a *API) GetMessageSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	unsignedMessage, err := a.db.Get(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s with error %w", messageID, err)
	}

	signature, err := a.signer.Sign(ctx, unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message %s with error %w", messageID, err)
	}
	return signature, nil
}

// GetBlockSignature returns the BLS signature associated with a blockID.
// It constructs a warp message with a Hash payload containing the blockID,
// then returns the signature for that message.
func (a *API) GetBlockSignature(ctx context.Context, blockID ids.ID) (hexutil.Bytes, error) {
	// Verify the block exists before signing
	if _, err := a.blockClient.GetBlock(ctx, blockID); err != nil {
		return nil, fmt.Errorf("failed to get block %s: %w", blockID, err)
	}

	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return nil, fmt.Errorf("failed to create block hash payload: %w", err)
	}

	unsignedMessage, err := warp.NewUnsignedMessage(
		a.chainContext.NetworkID,
		a.chainContext.ChainID,
		blockHashPayload.Bytes(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create unsigned warp message: %w", err)
	}

	signature, err := a.signer.Sign(ctx, unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign block %s with error %w", blockID, err)
	}
	return signature, nil
}

// GetMessageAggregateSignature fetches the aggregate signature for the requested [messageID]
func (a *API) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetIDStr string) (signedMessageBytes hexutil.Bytes, err error) {
	unsignedMessage, err := a.db.Get(messageID)
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

	validatorSet, err := validatorState.GetWarpValidatorSet(ctx, pChainHeight, subnetID)
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
		executor.WarpQuorumDenominator,
	)
	if err != nil {
		return nil, err
	}
	// TODO: return the signature and total weight as well to the caller for more complete details
	// Need to decide on the best UI for this and write up documentation with the potential
	// gotchas that could impact signed messages becoming invalid.
	return hexutil.Bytes(signedMessage.Bytes()), nil
}
