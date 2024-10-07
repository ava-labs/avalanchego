// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	ErrDuplicateValidator     = errors.New("duplicate validator")
	ErrInsufficientSignatures = errors.New("failed to aggregate sufficient stake weight of signatures")
)

type result struct {
	message *warp.Message
	err     error
}

type Validator struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	Weight    uint64
}

type indexedValidator struct {
	Validator
	I int
}

// NewSignatureAggregator returns an instance of SignatureAggregator
func NewSignatureAggregator(
	log logging.Logger,
	client *p2p.Client,
	maxPending int,
) *SignatureAggregator {
	return &SignatureAggregator{
		log:        log,
		client:     client,
		maxPending: int64(maxPending),
	}
}

// SignatureAggregator aggregates validator signatures for warp messages
type SignatureAggregator struct {
	log        logging.Logger
	client     *p2p.Client
	maxPending int64
}

// AggregateSignatures blocks until stakeWeightThreshold of validators signs the
// provided message. Validators are issued requests in the caller-specified
// order.
func (s *SignatureAggregator) AggregateSignatures(
	parentCtx context.Context,
	message *warp.UnsignedMessage,
	justification []byte,
	validators []Validator,
	stakeWeightThreshold uint64,
) (*warp.Message, error) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	request := &sdk.SignatureRequest{
		Message:       message.Bytes(),
		Justification: justification,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	done := make(chan result)
	pendingRequests := semaphore.NewWeighted(s.maxPending)
	lock := &sync.Mutex{}
	aggregatedStakeWeight := uint64(0)
	attemptedStakeWeight := uint64(0)
	totalStakeWeight := uint64(0)
	signatures := make([]*bls.Signature, 0)
	signerBitSet := set.NewBits()

	nodeIDsToValidator := make(map[ids.NodeID]indexedValidator)
	for i, v := range validators {
		totalStakeWeight += v.Weight

		// Sanity check the validator set provided by the caller
		if _, ok := nodeIDsToValidator[v.NodeID]; ok {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateValidator, v.NodeID)
		}

		nodeIDsToValidator[v.NodeID] = indexedValidator{
			I:         i,
			Validator: v,
		}
	}

	onResponse := func(
		_ context.Context,
		nodeID ids.NodeID,
		responseBytes []byte,
		err error,
	) {
		// We are guaranteed a response from a node in the validator set
		validator := nodeIDsToValidator[nodeID]

		defer func() {
			lock.Lock()
			attemptedStakeWeight += validator.Weight
			remainingStakeWeight := totalStakeWeight - attemptedStakeWeight
			failed := remainingStakeWeight < stakeWeightThreshold
			lock.Unlock()

			if failed {
				done <- result{err: ErrInsufficientSignatures}
			}

			pendingRequests.Release(1)
		}()

		if err != nil {
			s.log.Debug(
				"dropping response",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			return
		}

		response := &sdk.SignatureResponse{}
		if err := proto.Unmarshal(responseBytes, response); err != nil {
			s.log.Debug(
				"dropping response",
				zap.Stringer("nodeID", nodeID),
				zap.Error(err),
			)
			return
		}

		signature, err := bls.SignatureFromBytes(response.Signature)
		if err != nil {
			s.log.Debug(
				"dropping response",
				zap.Stringer("nodeID", nodeID),
				zap.String("reason", "invalid signature"),
				zap.Error(err),
			)
			return
		}

		if !bls.Verify(validator.PublicKey, signature, message.Bytes()) {
			s.log.Debug(
				"dropping response",
				zap.Stringer("nodeID", nodeID),
				zap.String("reason", "public key failed verification"),
			)
			return
		}

		lock.Lock()
		signerBitSet.Add(validator.I)
		signatures = append(signatures, signature)
		aggregatedStakeWeight += validator.Weight

		if aggregatedStakeWeight >= stakeWeightThreshold {
			aggregateSignature, err := bls.AggregateSignatures(signatures)
			if err != nil {
				done <- result{err: err}
				lock.Unlock()
				return
			}

			bitSetSignature := &warp.BitSetSignature{
				Signers:   signerBitSet.Bytes(),
				Signature: [bls.SignatureLen]byte{},
			}

			copy(bitSetSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
			signedMessage, err := warp.NewMessage(message, bitSetSignature)
			done <- result{message: signedMessage, err: err}
			lock.Unlock()
			return
		}

		lock.Unlock()
	}

	for _, validator := range validators {
		if err := pendingRequests.Acquire(ctx, 1); err != nil {
			return nil, err
		}

		// Avoid loop shadowing in goroutine
		validatorCopy := validator
		go func() {
			if err := s.client.AppRequest(
				ctx,
				set.Of(validatorCopy.NodeID),
				requestBytes,
				onResponse,
			); err != nil {
				done <- result{err: err}
				return
			}
		}()
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.message, r.err
	}
}
