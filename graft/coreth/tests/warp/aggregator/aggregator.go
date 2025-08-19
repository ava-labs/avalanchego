// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/coreth/precompile/contracts/warp"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

type AggregateSignatureResult struct {
	// Weight of validators included in the aggregate signature.
	SignatureWeight uint64
	// Total weight of all validators in the subnet.
	TotalWeight uint64
	// The message with the aggregate signature.
	Message *avalancheWarp.Message
}

type signatureFetchResult struct {
	sig    *bls.Signature
	index  int
	weight uint64
}

// Aggregator requests signatures from validators and
// aggregates them into a single signature.
type Aggregator struct {
	validators  []*avalancheWarp.Validator
	totalWeight uint64
	client      SignatureGetter
}

// New returns a signature aggregator that will attempt to aggregate signatures from [validators].
func New(client SignatureGetter, validators []*avalancheWarp.Validator, totalWeight uint64) *Aggregator {
	return &Aggregator{
		client:      client,
		validators:  validators,
		totalWeight: totalWeight,
	}
}

// Returns an aggregate signature over [unsignedMessage].
// The returned signature's weight exceeds the threshold given by [quorumNum].
func (a *Aggregator) AggregateSignatures(ctx context.Context, unsignedMessage *avalancheWarp.UnsignedMessage, quorumNum uint64) (*AggregateSignatureResult, error) {
	// Create a child context to cancel signature fetching if we reach signature threshold.
	signatureFetchCtx, signatureFetchCancel := context.WithCancel(ctx)
	defer signatureFetchCancel()

	// Fetch signatures from validators concurrently.
	signatureFetchResultChan := make(chan *signatureFetchResult)
	for i, validator := range a.validators {
		var (
			i         = i
			validator = validator
			// TODO: update from a single nodeID to the original slice and use extra nodeIDs as backup.
			nodeID = validator.NodeIDs[0]
		)
		go func() {
			log.Debug("Fetching warp signature",
				"nodeID", nodeID,
				"index", i,
				"msgID", unsignedMessage.ID(),
			)

			signature, err := a.client.GetSignature(signatureFetchCtx, nodeID, unsignedMessage)
			if err != nil {
				log.Debug("Failed to fetch warp signature",
					"nodeID", nodeID,
					"index", i,
					"err", err,
					"msgID", unsignedMessage.ID(),
				)
				signatureFetchResultChan <- nil
				return
			}

			log.Debug("Retrieved warp signature",
				"nodeID", nodeID,
				"msgID", unsignedMessage.ID(),
				"index", i,
			)

			if !bls.Verify(validator.PublicKey, signature, unsignedMessage.Bytes()) {
				log.Debug("Failed to verify warp signature",
					"nodeID", nodeID,
					"index", i,
					"msgID", unsignedMessage.ID(),
				)
				signatureFetchResultChan <- nil
				return
			}

			signatureFetchResultChan <- &signatureFetchResult{
				sig:    signature,
				index:  i,
				weight: validator.Weight,
			}
		}()
	}

	var (
		signatures                = make([]*bls.Signature, 0, len(a.validators))
		signersBitset             = set.NewBits()
		signaturesWeight          = uint64(0)
		signaturesPassedThreshold = false
	)

	for i := 0; i < len(a.validators); i++ {
		signatureFetchResult := <-signatureFetchResultChan
		if signatureFetchResult == nil {
			continue
		}

		signatures = append(signatures, signatureFetchResult.sig)
		signersBitset.Add(signatureFetchResult.index)
		signaturesWeight += signatureFetchResult.weight
		log.Debug("Updated weight",
			"totalWeight", signaturesWeight,
			"addedWeight", signatureFetchResult.weight,
			"msgID", unsignedMessage.ID(),
		)

		// If the signature weight meets the requested threshold, cancel signature fetching
		if err := avalancheWarp.VerifyWeight(signaturesWeight, a.totalWeight, quorumNum, warp.WarpQuorumDenominator); err == nil {
			log.Debug("Verify weight passed, exiting aggregation early",
				"quorumNum", quorumNum,
				"totalWeight", a.totalWeight,
				"signatureWeight", signaturesWeight,
				"msgID", unsignedMessage.ID(),
			)
			signatureFetchCancel()
			signaturesPassedThreshold = true
			break
		}
	}

	// If I failed to fetch sufficient signature stake, return an error
	if !signaturesPassedThreshold {
		return nil, avalancheWarp.ErrInsufficientWeight
	}

	// Otherwise, return the aggregate signature
	aggregateSignature, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate BLS signatures: %w", err)
	}

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signersBitset.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))

	msg, err := avalancheWarp.NewMessage(unsignedMessage, warpSignature)
	if err != nil {
		return nil, fmt.Errorf("failed to construct warp message: %w", err)
	}

	return &AggregateSignatureResult{
		Message:         msg,
		SignatureWeight: signaturesWeight,
		TotalWeight:     a.totalWeight,
	}, nil
}
