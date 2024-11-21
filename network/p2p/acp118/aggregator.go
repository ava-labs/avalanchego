// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"fmt"

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

// ErrFailedAggregation is returned if it's not possible for us to
// generate a signature
var (
	ErrFailedAggregation  = errors.New("failed aggregation")
	errFailedVerification = errors.New("failed verification")
)

// Validator signs warp messages. NodeID must be unique across validators, but
// PublicKey is not guaranteed to be unique.
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
	Validator indexedValidator
	Signature *bls.Signature
	Err       error
}

// NewSignatureAggregator returns an instance of SignatureAggregator
func NewSignatureAggregator(log logging.Logger, client *p2p.Client) *SignatureAggregator {
	return &SignatureAggregator{
		log:    log,
		client: client,
	}
}

// SignatureAggregator aggregates validator signatures for warp messages
type SignatureAggregator struct {
	log    logging.Logger
	client *p2p.Client
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

	nodeIDsToValidator := make(map[ids.NodeID]indexedValidator)
	// TODO expose concrete type to avoid type casting
	bitSetSignature, ok := message.Signature.(*warp.BitSetSignature)
	if !ok {
		return nil, 0, 0, errors.New("invalid warp signature type")
	}

	signerBitSet := set.BitsFromBytes(bitSetSignature.Signers)

	sampleable := make([]ids.NodeID, 0, len(validators))
	aggregatedStakeWeight := uint64(0)
	totalStakeWeight := uint64(0)
	for i, v := range validators {
		totalStakeWeight += v.Weight

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

	signatures := make([]*bls.Signature, 0, len(sampleable)+1)
	if bitSetSignature.Signature != [bls.SignatureLen]byte{} {
		blsSignature, err := bls.SignatureFromBytes(bitSetSignature.Signature[:])
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to parse bls signature: %w", err)
		}
		signatures = append(signatures, blsSignature)
	}

	results := make(chan result)
	job := responseHandler{
		message:            message,
		nodeIDsToValidator: nodeIDsToValidator,
		results:            results,
	}

	for _, nodeID := range sampleable {
		go func() {
			if err := s.client.AppRequest(ctx, set.Of(nodeIDCopy), requestBytes, job.HandleResponse); err != nil {
				results <- result{Validator: nodeIDsToValidator[nodeIDCopy], Err: err}
			}
		}()
	}

	failedStakeWeight := uint64(0)
	minThreshold := (totalStakeWeight * quorumNum) / quorumDen

	for {
		select {
		case <-ctx.Done():
			if len(signatures) == 0 {
				return message, 0, totalStakeWeight, nil
			}

			// Try to return whatever progress we have if the context is cancelled
			msg, err := newWarpMessage(message, signerBitSet, signatures)
			if err != nil {
				return nil, 0, 0, err
			}

			return msg, aggregatedStakeWeight, totalStakeWeight, nil
		case result := <-results:
			if result.Err != nil {
				s.log.Debug(
					"dropping response",
					zap.Stringer("nodeID", result.Validator.NodeID),
					zap.Uint64("weight", result.Validator.Weight),
					zap.Error(err),
				)

				// Fast-fail if it's not possible to generate a signature that meets the
				// minimum threshold
				failedStakeWeight += result.Validator.Weight
				if totalStakeWeight-failedStakeWeight < minThreshold {
					return nil, 0, 0, ErrFailedAggregation
				}
				continue
			}

			signatures = append(signatures, result.Signature)
			signerBitSet.Add(result.Validator.Index)
			aggregatedStakeWeight += result.Validator.Weight

			if aggregatedStakeWeight >= minThreshold {
				msg, err := newWarpMessage(message, signerBitSet, signatures)
				if err != nil {
					return nil, 0, 0, err
				}

				return msg, aggregatedStakeWeight, totalStakeWeight, nil
			}
		}
	}
}

func newWarpMessage(
	message *warp.Message,
	signerBitSet set.Bits,
	signatures []*bls.Signature,
) (*warp.Message, error) {
	aggregateSignature, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return nil, err
	}

	bitSetSignature := &warp.BitSetSignature{
		Signers:   signerBitSet.Bytes(),
		Signature: [bls.SignatureLen]byte{},
	}
	copy(bitSetSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))

	return warp.NewMessage(&message.UnsignedMessage, bitSetSignature)
}

type responseHandler struct {
	message            *warp.Message
	nodeIDsToValidator map[ids.NodeID]indexedValidator
	results            chan result
}

func (r *responseHandler) HandleResponse(
	_ context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	validator := r.nodeIDsToValidator[nodeID]

	if err != nil {
		r.results <- result{Validator: validator, Err: err}
		return
	}

	response := &sdk.SignatureResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		r.results <- result{Validator: validator, Err: err}
		return
	}

	signature, err := bls.SignatureFromBytes(response.Signature)
	if err != nil {
		r.results <- result{Validator: validator, Err: err}
		return
	}

	if !bls.Verify(validator.PublicKey, signature, r.message.UnsignedMessage.Bytes()) {
		r.results <- result{Validator: validator, Err: errFailedVerification}
		return
	}

	r.results <- result{Validator: validator, Signature: signature}
}
