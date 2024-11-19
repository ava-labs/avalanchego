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

// ErrFailedAggregation is returned if it's not possible for us to
// generate a signature
var ErrFailedAggregation = errors.New("failed aggregation")

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
	Message          *warp.Message
	AggregatedWeight uint64
	TotalWeight      uint64
	Err              error
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
// validators are requested to be aggregated into a warp message or the context
// is canceled. Returns the signed message and the amount of stake that signed
// the message. Caller is responsible for providing a well-formed canonical
// validator set corresponding to the signer bitset in the message.
func (s *SignatureAggregator) AggregateSignatures(
	ctx context.Context,
	message *warp.Message,
	justification []byte,
	validators []Validator,
	quorumNum uint64,
	quorumDen uint64,
) (*warp.Message, uint64, uint64, error) {
	request := &sdk.SignatureRequest{
		Message:       message.UnsignedMessage.Bytes(),
		Justification: justification,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	done := make(chan result)
	totalStakeWeight := uint64(0)
	nodeIDsToValidator := make(map[ids.NodeID]indexedValidator)
	// TODO expose concrete type to avoid type casting
	bitSetSignature, ok := message.Signature.(*warp.BitSetSignature)
	if !ok {
		return nil, 0, 0, errors.New("invalid warp signature type")
	}

	var signerBitSet set.Bits
	if bitSetSignature.Signers != nil {
		signerBitSet = set.BitsFromBytes(bitSetSignature.Signers)
	} else {
		signerBitSet = set.NewBits()
	}

	sampleable := make([]ids.NodeID, 0, len(validators))
	aggregatedStakeWeight := uint64(0)
	for i, v := range validators {
		totalStakeWeight, err = math.Add[uint64](totalStakeWeight, v.Weight)
		if err != nil {
			return nil, 0, 0, err
		}

		// Only try to aggregate signatures from validators that are not already in
		// the signer bit set
		if signerBitSet.Contains(i) {
			aggregatedStakeWeight += v.Weight
			continue
		}

		nodeIDsToValidator[v.NodeID] = indexedValidator{
			Index:     i,
			Validator: v,
		}
		sampleable = append(sampleable, v.NodeID)
	}

	signatures := make([]*bls.Signature, 0, 1)
	if bitSetSignature.Signature != [bls.SignatureLen]byte{} {
		blsSignature, err := bls.SignatureFromBytes(bitSetSignature.Signature[:])
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to parse bls signature: %w", err)
		}
		signatures = append(signatures, blsSignature)
	}

	job := aggregationJob{
		log:                   s.log,
		message:               message,
		minThreshold:          (totalStakeWeight * quorumNum) / quorumDen,
		nodeIDsToValidator:    nodeIDsToValidator,
		totalStakeWeight:      totalStakeWeight,
		aggregatedStakeWeight: aggregatedStakeWeight,
		signatures:            signatures,
		signerBitSet:          signerBitSet,
		done:                  done,
	}

	for _, nodeID := range sampleable {
		if err := s.client.AppRequest(ctx, set.Of(nodeID), requestBytes, job.HandleResponse); err != nil {
			return nil, 0, 0, err
		}
	}

	select {
	case <-ctx.Done():
		// Try to return whatever progress we have made if the context is
		// cancelled early
		result := job.GetResult()
		if result.Err != nil {
			return nil, 0, 0, result.Err
		}

		return result.Message, result.AggregatedWeight, result.TotalWeight, nil
	case result := <-done:
		if result.Err != nil {
			return nil, 0, 0, result.Err
		}

		return result.Message, result.AggregatedWeight, result.TotalWeight, nil
	}
}

type aggregationJob struct {
	log                logging.Logger
	message            *warp.Message
	minThreshold       uint64
	nodeIDsToValidator map[ids.NodeID]indexedValidator
	totalStakeWeight   uint64

	lock                  sync.Mutex
	aggregatedStakeWeight uint64
	failedStakeWeight     uint64
	signatures            []*bls.Signature
	signerBitSet          set.Bits

	done chan result
}

func (a *aggregationJob) GetResult() result {
	a.lock.Lock()
	defer a.lock.Unlock()

	return a.getResult()
}

func (a *aggregationJob) getResult() result {
	aggregateSignature, err := bls.AggregateSignatures(a.signatures)
	if err != nil {
		return result{Err: fmt.Errorf("%w: %w", ErrFailedAggregation, err)}
	}

	bitSetSignature := &warp.BitSetSignature{
		Signers:   a.signerBitSet.Bytes(),
		Signature: [bls.SignatureLen]byte{},
	}

	copy(bitSetSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))
	unsignedMessage, err := warp.NewUnsignedMessage(a.message.NetworkID, a.message.SourceChainID, a.message.Payload)
	if err != nil {
		return result{Err: fmt.Errorf("failed to initialize message: %w", err)}
	}

	msg, err := warp.NewMessage(unsignedMessage, bitSetSignature)
	if err != nil {
		return result{Err: err}
	}

	return result{
		Message:          msg,
		AggregatedWeight: a.aggregatedStakeWeight,
		TotalWeight:      a.totalStakeWeight,
	}
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

	defer a.lock.Unlock()
	defer func() {
		if a.aggregatedStakeWeight >= a.minThreshold {
			a.done <- a.getResult()
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
