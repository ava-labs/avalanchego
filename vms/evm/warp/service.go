// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

var (
	errNoValidators    = errors.New("cannot aggregate signatures from subnet with no validators")
	ErrMessageNotFound = errors.New("message not found")
	ErrBlockNotFound   = errors.New("block not found")
)

// API introduces snowman specific functionality to the evm.
// It provides caching and orchestration over the core warp primitives.
type API struct {
	chainContext        *snow.Context
	db                  *DB
	signer              warp.Signer
	verifier            *Verifier
	signatureAggregator *acp118.SignatureAggregator

	// Caching
	messageCache     *lru.Cache[ids.ID, *warp.UnsignedMessage]
	signatureCache   cache.Cacher[ids.ID, []byte]
	offchainMessages map[ids.ID]*warp.UnsignedMessage
}

func NewAPI(
	chainCtx *snow.Context,
	db *DB,
	signer warp.Signer,
	verifier *Verifier,
	signatureCache cache.Cacher[ids.ID, []byte],
	signatureAggregator *acp118.SignatureAggregator,
	offchainMessages [][]byte,
) (*API, error) {
	offchainMsgs := make(map[ids.ID]*warp.UnsignedMessage)
	for i, offchainMsg := range offchainMessages {
		unsignedMsg, err := warp.ParseUnsignedMessage(offchainMsg)
		if err != nil {
			return nil, fmt.Errorf("failed to parse off-chain message at index %d: %w", i, err)
		}

		if unsignedMsg.NetworkID != chainCtx.NetworkID {
			return nil, fmt.Errorf("wrong network ID at index %d", i)
		}

		if unsignedMsg.SourceChainID != chainCtx.ChainID {
			return nil, fmt.Errorf("wrong source chain ID at index %d", i)
		}

		_, err = payload.ParseAddressedCall(unsignedMsg.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to parse off-chain message at index %d as AddressedCall: %w", i, err)
		}
		offchainMsgs[unsignedMsg.ID()] = unsignedMsg
	}

	return &API{
		db:                  db,
		signer:              signer,
		verifier:            verifier,
		chainContext:        chainCtx,
		signatureAggregator: signatureAggregator,
		messageCache:        lru.NewCache[ids.ID, *warp.UnsignedMessage](500),
		signatureCache:      signatureCache,
		offchainMessages:    offchainMsgs,
	}, nil
}

// GetMessage returns the Warp message associated with a messageID.
func (a *API) GetMessage(_ context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	message, err := a.getMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s: %w", messageID, err)
	}
	return hexutil.Bytes(message.Bytes()), nil
}

// getMessage retrieves a message from cache, offchain messages, or database.
func (a *API) getMessage(messageID ids.ID) (*warp.UnsignedMessage, error) {
	if msg, ok := a.messageCache.Get(messageID); ok {
		return msg, nil
	}

	if msg, ok := a.offchainMessages[messageID]; ok {
		return msg, nil
	}

	msg, err := a.db.Get(messageID)
	if err != nil {
		return nil, err
	}

	a.messageCache.Put(messageID, msg)
	return msg, nil
}

// GetMessageSignature returns the BLS signature associated with a messageID.
func (a *API) GetMessageSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	unsignedMessage, err := a.getMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrMessageNotFound, messageID, err)
	}
	return a.signMessage(ctx, unsignedMessage)
}

// GetBlockSignature returns the BLS signature associated with a blockID.
// It constructs a warp message with a Hash payload containing the blockID,
// then returns the signature for that message.
func (a *API) GetBlockSignature(ctx context.Context, blockID ids.ID) (hexutil.Bytes, error) {
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

	return a.signMessage(ctx, unsignedMessage)
}

// GetMessageAggregateSignature fetches the aggregate signature for the requested [messageID]
func (a *API) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetIDStr string) (signedMessageBytes hexutil.Bytes, err error) {
	unsignedMessage, err := a.getMessage(messageID)
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

// signMessage verifies, signs, and caches a signature for the given unsigned message.
func (a *API) signMessage(ctx context.Context, unsignedMessage *warp.UnsignedMessage) (hexutil.Bytes, error) {
	msgID := unsignedMessage.ID()

	if sig, ok := a.signatureCache.Get(msgID); ok {
		return sig, nil
	}

	if err := a.verifier.Verify(ctx, unsignedMessage); err != nil {
		if err.Code == VerifyErrCode {
			return nil, fmt.Errorf("%w: %w", ErrBlockNotFound, err)
		}
		return nil, fmt.Errorf("failed to verify message %s: %w", msgID, err)
	}

	signature, err := a.signer.Sign(unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message %s: %w", msgID, err)
	}

	a.signatureCache.Put(msgID, signature)
	return signature, nil
}
