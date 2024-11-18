// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/utils/math"
	"go.uber.org/zap"
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
	Signature *warp.BitSetSignature
	Err       error
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
	message *warp.UnsignedMessage,
	justification []byte,
	validators []Validator,
	quorumNumMin uint64,
	quorumDenMin uint64,
	quorumNumMax uint64,
	quorumDenMax uint64,
) (*warp.Message, error) {
	request := &sdk.SignatureRequest{
		Message:       message.Bytes(),
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
	}

	minThreshold := (totalStakeWeight * quorumNumMin) / quorumDenMin
	maxThreshold := (totalStakeWeight * quorumNumMax) / quorumDenMax

	job := aggregationJob{
		log:                s.log,
		client:             s.client,
		requestBytes:       requestBytes,
		messageBytes:       message.Bytes(),
		minThreshold:       minThreshold,
		maxThreshold:       maxThreshold,
		validators:         validators,
		nodeIDsToValidator: nodeIDsToValidator,
		totalStakeWeight:   totalStakeWeight,
		signatures:         make([]*bls.Signature, 0),
		signerBitSet:       set.NewBits(),
		sem:                sem,
		done:               done,
	}

	for {
		select {
		case <-ctx.Done():
			// Try to return whatever progress we have made if the context is
			// cancelled early
			signature, err := job.GetResult()
			if err != nil {
				return nil, err
			}

			return warp.NewMessage(message, signature)
		case result := <-done:
			if result.Err != nil {
				return nil, result.Err
			}

			return warp.NewMessage(message, result.Signature)
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
	messageBytes       []byte
	minThreshold       uint64
	maxThreshold       uint64
	validators         []Validator
	index              int
	nodeIDsToValidator map[ids.NodeID]indexedValidator
	totalStakeWeight   uint64

	lock                  sync.Mutex
	aggregatedStakeWeight uint64
	failedStakeWeight     uint64
	signatures            []*bls.Signature
	signerBitSet          set.Bits

	sem  <-chan struct{}
	done chan result
}

func (a *aggregationJob) GetResult() (*warp.BitSetSignature, error) {
	a.lock.Lock()
	defer a.lock.Unlock()

	return a.getResult()
}

func (a *aggregationJob) getResult() (*warp.BitSetSignature, error) {
	aggregateSignature, err := bls.AggregateSignatures(a.signatures)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailedAggregation, err)
	}

	bitSetSignature := &warp.BitSetSignature{
		Signers:   a.signerBitSet.Bytes(),
		Signature: [bls.SignatureLen]byte{},
	}

	copy(bitSetSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	return bitSetSignature, nil
}

func (a *aggregationJob) SendRequest(ctx context.Context) error {
	if len(a.validators) == 0 {
		return nil
	}

	if err := a.client.AppRequest(ctx, set.Of(a.validators[a.index].NodeID), a.requestBytes, a.HandleResponse); err != nil {
		return err
	}

	a.index++

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

		if failed {
			a.failedStakeWeight += validator.Weight
		}

		// Fast-fail if it's not possible to generate a signature that meets the
		// minimum threshold
		if a.totalStakeWeight-a.failedStakeWeight < a.minThreshold {
			a.done <- result{Err: ErrFailedAggregation}
			return
		}

		if a.aggregatedStakeWeight >= a.maxThreshold {
			signature, err := a.getResult()
			a.done <- result{Signature: signature, Err: err}
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

	if !bls.Verify(validator.PublicKey, signature, a.messageBytes) {
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
