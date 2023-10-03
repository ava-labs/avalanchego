// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/subnet-evm/params"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var errNoValidators = errors.New("cannot aggregate signatures from subnet with no validators")

type AggregateSignatureResult struct {
	// Weight of validators included in the aggregate signature.
	SignatureWeight uint64
	// Total weight of all validators in the subnet.
	TotalWeight uint64
	// The message with the aggregate signature.
	Message *avalancheWarp.Message
}

// Aggregator requests signatures from validators and
// aggregates them into a single signature.
type Aggregator struct {
	// Aggregating signatures for a chain validated by this subnet.
	subnetID ids.ID
	// Fetches signatures from validators.
	client SignatureGetter
	// Validator state for this chain.
	state validators.State
}

// New returns a signature aggregator for the chain with the given [state] on the
// given [subnetID], and where [client] can be used to fetch signatures from validators.
func New(subnetID ids.ID, state validators.State, client SignatureGetter) *Aggregator {
	return &Aggregator{
		subnetID: subnetID,
		client:   client,
		state:    state,
	}
}

// Returns an aggregate signature over [unsignedMessage].
// The returned signature's weight exceeds the threshold given by [quorumNum].
func (a *Aggregator) AggregateSignatures(ctx context.Context, unsignedMessage *avalancheWarp.UnsignedMessage, quorumNum uint64) (*AggregateSignatureResult, error) {
	// Note: we use the current height as a best guess of the canonical validator set when the aggregated signature will be verified
	// by the recipient chain. If the validator set changes from [pChainHeight] to the P-Chain height that is actually specified by the
	// ProposerVM header when this message is verified, then the aggregate signature could become outdated and require re-aggregation.
	pChainHeight, err := a.state.GetCurrentHeight(ctx)
	if err != nil {
		return nil, err
	}

	log.Debug("Fetching signature",
		"a.subnetID", a.subnetID,
		"height", pChainHeight,
	)
	validators, totalWeight, err := avalancheWarp.GetCanonicalValidatorSet(ctx, a.state, pChainHeight, a.subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set: %w", err)
	}
	if len(validators) == 0 {
		return nil, fmt.Errorf("%w (SubnetID: %s, Height: %d)", errNoValidators, a.subnetID, pChainHeight)
	}

	type signatureFetchResult struct {
		sig    *bls.Signature
		index  int
		weight uint64
	}

	// Create a child context to cancel signature fetching if we reach signature threshold.
	signatureFetchCtx, signatureFetchCancel := context.WithCancel(ctx)
	defer signatureFetchCancel()

	// Fetch signatures from validators concurrently.
	signatureFetchResultChan := make(chan *signatureFetchResult)
	for i, validator := range validators {
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
		signatures                = make([]*bls.Signature, 0, len(validators))
		signersBitset             = set.NewBits()
		signaturesWeight          = uint64(0)
		signaturesPassedThreshold = false
	)

	for i := 0; i < len(validators); i++ {
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
		if err := avalancheWarp.VerifyWeight(signaturesWeight, totalWeight, quorumNum, params.WarpQuorumDenominator); err == nil {
			log.Debug("Verify weight passed, exiting aggregation early",
				"quorumNum", quorumNum,
				"totalWeight", totalWeight,
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
		TotalWeight:     totalWeight,
	}, nil
}
