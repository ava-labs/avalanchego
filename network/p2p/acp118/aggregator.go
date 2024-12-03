// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp118

import (
	"context"
	"errors"
	"fmt"
	"math/big"

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

var errFailedVerification = errors.New("failed verification")

type indexedValidator struct {
	*warp.Validator
	Index int
}

type result struct {
	NodeID    ids.NodeID
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
	validators []*warp.Validator,
	quorumNum uint64,
	quorumDen uint64,
) (
	_ *warp.Message,
	aggregatedStake *big.Int,
	totalStake *big.Int,
	finished bool,
	_ error,
) {
	request := &sdk.SignatureRequest{
		Message:       message.UnsignedMessage.Bytes(),
		Justification: justification,
	}

	requestBytes, err := proto.Marshal(request)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("failed to marshal signature request: %w", err)
	}

	publicKeysToValidators := make(map[*bls.PublicKey]indexedValidator)
	nodeIDsToPublicKeys := make(map[ids.NodeID]*bls.PublicKey)
	// TODO expose concrete type to avoid type casting
	bitSetSignature, ok := message.Signature.(*warp.BitSetSignature)
	if !ok {
		return nil, nil, nil, false, errors.New("invalid warp signature type")
	}

	signerBitSet := set.BitsFromBytes(bitSetSignature.Signers)

	nonSigners := make([]*bls.PublicKey, 0, len(validators))
	aggregatedStakeWeight := new(big.Int)
	totalStakeWeight := new(big.Int)
	numRequests := 0
	for i, v := range validators {
		totalStakeWeight.Add(totalStakeWeight, new(big.Int).SetUint64(v.Weight))

		// Only try to aggregate signatures from validators that are not already in
		// the signer bit set
		if signerBitSet.Contains(i) {
			aggregatedStakeWeight.Add(aggregatedStakeWeight, new(big.Int).SetUint64(v.Weight))
			continue
		}

		publicKeysToValidators[v.PublicKey] = indexedValidator{
			Index:     i,
			Validator: v,
		}

		for _, nodeID := range v.NodeIDs {
			numRequests += 1
			nodeIDsToPublicKeys[nodeID] = v.PublicKey
		}

		nonSigners = append(nonSigners, v.PublicKey)
	}

	signatures := make([]*bls.Signature, 0, len(nonSigners)+1)
	if bitSetSignature.Signature != [bls.SignatureLen]byte{} {
		blsSignature, err := bls.SignatureFromBytes(bitSetSignature.Signature[:])
		if err != nil {
			return nil, nil, nil, false, fmt.Errorf("failed to parse bls signature: %w", err)
		}
		signatures = append(signatures, blsSignature)
	}

	results := make(chan result)
	handler := responseHandler{
		message:                message,
		publicKeysToValidators: publicKeysToValidators,
		nodeIDsToPublicKeys:    nodeIDsToPublicKeys,
		results:                results,
	}

	for _, pk := range nonSigners {
		validator := publicKeysToValidators[pk]
		for _, nodeID := range validator.NodeIDs {
			if err := s.client.AppRequest(ctx, set.Of(nodeID), requestBytes, handler.HandleResponse); err != nil {
				return nil, nil, nil, false, fmt.Errorf("failed to send aggregation request: %w", err)
			}
		}
	}

	minThreshold := new(big.Int).Mul(totalStakeWeight, new(big.Int).SetUint64(quorumNum))
	minThreshold.Div(minThreshold, new(big.Int).SetUint64(quorumDen))

	for i := 0; i < numRequests; i++ {
		select {
		case <-ctx.Done():
			// Try to return whatever progress we have if the context is cancelled
			msg, err := newWarpMessage(message, signerBitSet, signatures)
			if err != nil {
				return nil, nil, nil, false, err
			}

			return msg, aggregatedStakeWeight, totalStakeWeight, false, nil
		case result := <-results:
			if result.Err != nil {
				s.log.Debug(
					"dropping response",
					zap.Stringer("nodeID", result.NodeID),
					zap.Error(err),
				)
				continue
			}

			if signerBitSet.Contains(result.Validator.Index) {
				s.log.Debug(
					"dropping duplicate signature",
					zap.Stringer("nodeID", result.NodeID),
					zap.Error(err),
				)
				continue
			}

			signatures = append(signatures, result.Signature)
			signerBitSet.Add(result.Validator.Index)
			aggregatedStakeWeight.Add(aggregatedStakeWeight, new(big.Int).SetUint64(result.Validator.Weight))

			if aggregatedStakeWeight.Cmp(minThreshold) != -1 {
				msg, err := newWarpMessage(message, signerBitSet, signatures)
				if err != nil {
					return nil, nil, nil, false, err
				}

				return msg, aggregatedStakeWeight, totalStakeWeight, true, nil
			}
		}
	}

	msg, err := newWarpMessage(message, signerBitSet, signatures)
	if err != nil {
		return nil, nil, nil, false, err
	}

	return msg, aggregatedStakeWeight, totalStakeWeight, true, nil
}

func newWarpMessage(
	message *warp.Message,
	signerBitSet set.Bits,
	signatures []*bls.Signature,
) (*warp.Message, error) {
	if len(signatures) == 0 {
		return message, nil
	}

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
	message                *warp.Message
	publicKeysToValidators map[*bls.PublicKey]indexedValidator
	nodeIDsToPublicKeys    map[ids.NodeID]*bls.PublicKey
	results                chan result
}

func (r *responseHandler) HandleResponse(
	_ context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) {
	validator := r.publicKeysToValidators[r.nodeIDsToPublicKeys[nodeID]]
	if err != nil {
		r.results <- result{NodeID: nodeID, Validator: validator, Err: err}
		return
	}

	response := &sdk.SignatureResponse{}
	if err := proto.Unmarshal(responseBytes, response); err != nil {
		r.results <- result{NodeID: nodeID, Validator: validator, Err: err}
		return
	}

	signature, err := bls.SignatureFromBytes(response.Signature)
	if err != nil {
		r.results <- result{NodeID: nodeID, Validator: validator, Err: err}
		return
	}

	if !bls.Verify(validator.PublicKey, signature, r.message.UnsignedMessage.Bytes()) {
		r.results <- result{NodeID: nodeID, Validator: validator, Err: errFailedVerification}
		return
	}

	r.results <- result{NodeID: nodeID, Validator: validator, Signature: signature}
}
