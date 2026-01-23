// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	evmwarp "github.com/ava-labs/avalanchego/vms/evm/warp"
)

var (
	errNoValidators    = errors.New("cannot aggregate signatures from subnet with no validators")
	ErrMessageNotFound = errors.New("message not found")
	ErrBlockNotFound   = errors.New("block not found")
)

// Service introduces snowman specific functionality to the evm.
// It provides caching and orchestration over the core warp primitives.
type Service struct {
	networkID      uint32
	chainID        ids.ID
	validatorState validators.State

	db                  *evmwarp.DB
	signer              warp.Signer
	verifier            *evmwarp.Verifier
	signatureAggregator *acp118.SignatureAggregator

	messageCache     *lru.Cache[ids.ID, *warp.UnsignedMessage]
	signatureCache   cache.Cacher[ids.ID, []byte]
	offChainMsgs evmwarp.OffChainMessages
}

func NewService(
	networkID uint32,
	chainID ids.ID,
	validatorState validators.State,
	db *evmwarp.DB,
	signer warp.Signer,
	verifier *evmwarp.Verifier,
	signatureCache cache.Cacher[ids.ID, []byte],
	signatureAggregator *acp118.SignatureAggregator,
	offChainMsgs evmwarp.OffChainMessages,
) (*Service, error) {
	return &Service{
		networkID:           networkID,
		chainID:             chainID,
		validatorState:      validatorState,
		db:                  db,
		signer:              signer,
		verifier:            verifier,
		signatureAggregator: signatureAggregator,
		messageCache:        lru.NewCache[ids.ID, *warp.UnsignedMessage](500),
		signatureCache:      signatureCache,
		offChainMsgs:    offChainMsgs,
	}, nil
}

// GetMessage returns the Warp message associated with the given messageID.
func (s *Service) GetMessage(messageID ids.ID) (hexutil.Bytes, error) {
	message, err := s.getMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message %s: %w", messageID, err)
	}
	return hexutil.Bytes(message.Bytes()), nil
}

// getMessage retrieves a message from cache, offchain messages, or database.
func (s *Service) getMessage(messageID ids.ID) (*warp.UnsignedMessage, error) {
	if msg, ok := s.messageCache.Get(messageID); ok {
		return msg, nil
	}

	if msg, ok := s.offChainMsgs.Get(messageID); ok {
		return msg, nil
	}

	msg, err := s.db.Get(messageID)
	if err != nil {
		return nil, err
	}

	s.messageCache.Put(messageID, msg)
	return msg, nil
}

// GetMessageSignature returns the BLS signature associated with the given messageID.
func (s *Service) GetMessageSignature(ctx context.Context, messageID ids.ID) (hexutil.Bytes, error) {
	unsignedMessage, err := s.getMessage(messageID)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrMessageNotFound, messageID, err)
	}
	return s.signMessage(ctx, unsignedMessage)
}

// GetBlockSignature returns the BLS signature associated with the given blockID.
// It constructs a warp message with a Hash payload containing the blockID,
// then returns the signature for that message.
func (s *Service) GetBlockSignature(ctx context.Context, blockID ids.ID) (hexutil.Bytes, error) {
	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return nil, fmt.Errorf("failed to create block hash payload: %w", err)
	}

	unsignedMessage, err := warp.NewUnsignedMessage(
		s.networkID,
		s.chainID,
		blockHashPayload.Bytes(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create unsigned warp message: %w", err)
	}

	return s.signMessage(ctx, unsignedMessage)
}

// GetMessageAggregateSignature fetches the aggregate signature for the requested messageID.
func (s *Service) GetMessageAggregateSignature(ctx context.Context, messageID ids.ID, quorumNum uint64, subnetID ids.ID) (signedMessageBytes hexutil.Bytes, err error) {
	unsignedMessage, err := s.getMessage(messageID)
	if err != nil {
		return nil, err
	}
	return s.aggregateSignatures(ctx, unsignedMessage, quorumNum, subnetID)
}

// GetBlockAggregateSignature fetches the aggregate signature for the requested blockID.
func (s *Service) GetBlockAggregateSignature(ctx context.Context, blockID ids.ID, quorumNum uint64, subnetID ids.ID) (signedMessageBytes hexutil.Bytes, err error) {
	blockHashPayload, err := payload.NewHash(blockID)
	if err != nil {
		return nil, err
	}
	unsignedMessage, err := warp.NewUnsignedMessage(s.networkID, s.chainID, blockHashPayload.Bytes())
	if err != nil {
		return nil, err
	}

	return s.aggregateSignatures(ctx, unsignedMessage, quorumNum, subnetID)
}

func (s *Service) aggregateSignatures(ctx context.Context, unsignedMessage *warp.UnsignedMessage, quorumNum uint64, subnetID ids.ID) (hexutil.Bytes, error) {
	validatorState := s.validatorState
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
	signedMessage, _, _, err := s.signatureAggregator.AggregateSignatures(
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
func (s *Service) signMessage(ctx context.Context, unsignedMessage *warp.UnsignedMessage) (hexutil.Bytes, error) {
	msgID := unsignedMessage.ID()

	if sig, ok := s.signatureCache.Get(msgID); ok {
		return sig, nil
	}

	if err := s.verifier.Verify(ctx, unsignedMessage); err != nil {
		return nil, fmt.Errorf("failed to verify message %s: %w", msgID, err)
	}

	signature, err := s.signer.Sign(unsignedMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to sign message %s: %w", msgID, err)
	}

	s.signatureCache.Put(msgID, signature)
	return signature, nil
}
