// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	// ErrDuplicateValidator is returned if the provided validator set contains
	// duplicate validators
	ErrDuplicateValidator = errors.New("duplicate validator")
	// ErrFailedAggregation is returned if it's not possible for us to
	// generate a signature
	ErrFailedAggregation = errors.New("failed aggregation")
)

type Validator struct {
	NodeID    ids.NodeID
	PublicKey *bls.PublicKey
	Weight    uint64
}

type indexedValidator struct {
	Validator
	Index int
}

type result struct {
	Message *warp.Message
	Err     error
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
		maxPending: maxPending,
	}
}

// SignatureAggregator aggregates validator signatures for warp messages
type SignatureAggregator struct {
	log        logging.Logger
	client     *p2p.Client
	maxPending int
}

// AggregateSignatures blocks until quorumNum/quorumDen signatures from
// validators are requested to be aggregated into a warp message.
func (s *SignatureAggregator) AggregateSignatures(
	ctx context.Context,
	message *warp.Message,
	justification []byte,
	validators []Validator,
	quorumNum uint64,
	quorumDen uint64,
) (*warp.Message, error) {
	request := &sdk.SignatureRequest{
		Message:       message.UnsignedMessage.Bytes(),
		Justification: justification,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	done := make(chan result)
	sem := make(chan struct{}, s.maxPending)

	totalStakeWeight := uint64(0)
	nodeIDsToValidator := make(map[ids.NodeID]indexedValidator)
	// TODO expose concrete type to avoid type casting
	bitSetSignature, ok := message.Signature.(*warp.BitSetSignature)
	if !ok {
		return nil, fmt.Errorf("invalid warp signature type")
	}

	var signerBitSet set.Bits
	if bitSetSignature.Signers != nil {
		signerBitSet = set.BitsFromBytes(bitSetSignature.Signers)
	} else {
		signerBitSet = set.NewBits()
	}

	aggregatedStakeWeight := uint64(0)
	for i, v := range validators {
		totalStakeWeight, err = math.Add[uint64](totalStakeWeight, v.Weight)
		if err != nil {
			return nil, err
		}

		// Check that the validator set provided is valid
		if _, ok := nodeIDsToValidator[v.NodeID]; ok {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateValidator, v.NodeID)
		}

		nodeIDsToValidator[v.NodeID] = indexedValidator{
			Index:     i,
			Validator: v,
		}

		if signerBitSet.Contains(i) {
			aggregatedStakeWeight += v.Weight
		}
	}

	signatures := make([]*bls.Signature, 0, 1)
	if bitSetSignature.Signature != [bls.SignatureLen]byte{} {
		blsSignature, err := bls.SignatureFromBytes(bitSetSignature.Signature[:])
		if err != nil {
			return nil, fmt.Errorf("failed to parse bls signature: %w", err)
		}
		signatures = append(signatures, blsSignature)
	}

	job := aggregationJob{
		log:                   s.log,
		client:                s.client,
		requestBytes:          requestBytes,
		message:               message,
		minThreshold:          (totalStakeWeight * quorumNum) / quorumDen,
		nodeIDsToValidator:    nodeIDsToValidator,
		totalStakeWeight:      totalStakeWeight,
		aggregatedStakeWeight: aggregatedStakeWeight,
		signatures:            signatures,
		signerBitSet:          signerBitSet,
		sem:                   sem,
		done:                  done,
	}

	for {
		select {
		case <-ctx.Done():
			// Try to return whatever progress we have made if the context is
			// cancelled early
			result, err := job.GetResult()
			if err != nil {
				return nil, err
			}

			return result, nil
		case result := <-done:
			if result.Err != nil {
				return nil, result.Err
			}

			return result.Message, nil
		case sem <- struct{}{}:
			if err := job.SendRequest(ctx); err != nil {
				return nil, err
			}
		}
	}
}

type aggregationJob struct {
	log                logging.Logger
	client             *p2p.Client
	requestBytes       []byte
	message            *warp.Message
	minThreshold       uint64
	nodeIDsToValidator map[ids.NodeID]indexedValidator
	totalStakeWeight   uint64 // TODO initialize this correctly

	lock                  sync.Mutex
	aggregatedStakeWeight uint64
	failedStakeWeight     uint64
	signatures            []*bls.Signature
	signerBitSet          set.Bits

	sem  <-chan struct{}
	done chan result
}

func (a *aggregationJob) GetResult() (*warp.Message, error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	return a.getResult()
}

func (a *aggregationJob) getResult() (*warp.Message, error) {
	aggregateSignature, err := bls.AggregateSignatures(a.signatures)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedAggregation, err)
	}

	bitSetSignature := &warp.BitSetSignature{
		Signers:   a.signerBitSet.Bytes(),
		Signature: [bls.SignatureLen]byte{},
	}

	copy(bitSetSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	unsignedMessage, err := warp.NewUnsignedMessage(a.message.NetworkID, a.message.SourceChainID, a.message.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize message")
	}

	return warp.NewMessage(unsignedMessage, bitSetSignature)
}

func (a *aggregationJob) SendRequest(ctx context.Context) error {
	// TODO dont rely on map iteration order and use weighted sampling instead
	for nodeID, validator := range a.nodeIDsToValidator {
		if a.signerBitSet.Contains(validator.Index) {
			continue
		}

		if err := a.client.AppRequest(ctx, set.Of(nodeID), a.requestBytes, a.HandleResponse); err != nil {
			return err
		}
	}

	return nil
}

func (a *aggregationJob) HandleResponse(
	_ context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	a.lock.Lock()
	// We are guaranteed a response from a node in the validator set
	validator := a.nodeIDsToValidator[nodeID]
	failed := true

	defer func() {
		defer func() {
			a.lock.Unlock()
			<-a.sem
		}()

		if a.aggregatedStakeWeight >= a.minThreshold {
			signature, err := a.getResult()
			a.done <- result{Message: signature, Err: err}
			return
		}

		if failed {
			a.failedStakeWeight += validator.Weight
		}

		// Fast-fail if it's not possible to generate a signature that meets the
		// minimum threshold
		if a.totalStakeWeight-a.failedStakeWeight < a.minThreshold {
			a.done <- result{Err: ErrFailedAggregation}
			return
		}
	}()

	if err != nil {
		a.log.Debug(
			"dropping response",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
		return
	}

	response := &sdk.SignatureResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		a.log.Debug(
			"dropping response",
			zap.Stringer("nodeID", nodeID),
			zap.Error(err),
		)
		return
	}

	signature, err := bls.SignatureFromBytes(response.Signature)
	if err != nil {
		a.log.Debug(
			"dropping response",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "invalid signature"),
			zap.Error(err),
		)
		return
	}

	if !bls.Verify(validator.PublicKey, signature, a.message.UnsignedMessage.Bytes()) {
		a.log.Debug(
			"dropping response",
			zap.Stringer("nodeID", nodeID),
			zap.String("reason", "public key failed verification"),
		)
		return
	}

	failed = false
	a.signerBitSet.Add(validator.Index)
	a.signatures = append(a.signatures, signature)
	a.aggregatedStakeWeight += validator.Weight
}
