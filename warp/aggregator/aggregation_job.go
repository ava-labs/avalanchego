// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package aggregator

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/log"
)

// signatureAggregationJob fetches signatures for a single unsigned warp message.
type signatureAggregationJob struct {
	// SignatureBackend is assumed to be thread-safe and may be used by multiple signature aggregation jobs concurrently
	client   SignatureBackend
	height   uint64
	subnetID ids.ID

	// Minimum threshold at which to return the resulting aggregate signature. If this threshold is not reached,
	// return an error instead of aggregating the signatures that were fetched.
	minValidQuorumNum uint64
	// Threshold at which to cancel fetching further signatures
	maxNeededQuorumNum uint64
	// Denominator to use when checking if we've reached the threshold
	quorumDen uint64
	state     validators.State
	msg       *avalancheWarp.UnsignedMessage
}

type AggregateSignatureResult struct {
	SignatureWeight uint64
	TotalWeight     uint64
	Message         *avalancheWarp.Message
}

func newSignatureAggregationJob(
	client SignatureBackend,
	height uint64,
	subnetID ids.ID,
	minValidQuorumNum uint64,
	maxNeededQuorumNum uint64,
	quorumDen uint64,
	state validators.State,
	msg *avalancheWarp.UnsignedMessage,
) *signatureAggregationJob {
	return &signatureAggregationJob{
		client:             client,
		height:             height,
		subnetID:           subnetID,
		minValidQuorumNum:  minValidQuorumNum,
		maxNeededQuorumNum: maxNeededQuorumNum,
		quorumDen:          quorumDen,
		state:              state,
		msg:                msg,
	}
}

// Execute aggregates signatures for the requested message
func (a *signatureAggregationJob) Execute(ctx context.Context) (*AggregateSignatureResult, error) {
	log.Info("Fetching signature", "subnetID", a.subnetID, "height", a.height)
	validators, totalWeight, err := avalancheWarp.GetCanonicalValidatorSet(ctx, a.state, a.height, a.subnetID)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator set: %w", err)
	}
	if len(validators) == 0 {
		return nil, fmt.Errorf("cannot aggregate signatures from subnet with no validators (SubnetID: %s, Height: %d)", a.subnetID, a.height)
	}

	signatureJobs := make([]*signatureJob, len(validators))
	for i, validator := range validators {
		signatureJobs[i] = newSignatureJob(a.client, validator, a.msg)
	}

	var (
		// [signatureLock] must be held when accessing [blsSignatures], [bitSet], or [signatureWeight]
		// in the goroutine below.
		signatureLock   sync.Mutex
		blsSignatures   = make([]*bls.Signature, 0, len(signatureJobs))
		bitSet          = set.NewBits()
		signatureWeight = uint64(0)
	)

	// Create a child context to cancel signature fetching if we reach [maxNeededQuorumNum] threshold
	signatureFetchCtx, signatureFetchCancel := context.WithCancel(ctx)
	defer signatureFetchCancel()

	wg := sync.WaitGroup{}
	wg.Add(len(signatureJobs))
	for i, signatureJob := range signatureJobs {
		i := i
		signatureJob := signatureJob
		go func() {
			defer wg.Done()

			log.Info("Fetching warp signature",
				"nodeID", signatureJob.nodeID,
				"index", i,
			)

			blsSignature, err := signatureJob.Execute(signatureFetchCtx)
			if err != nil {
				log.Info("Failed to fetch signature at index %d: %s", i, signatureJob)
				return
			}
			log.Info("Retrieved warp signature",
				"nodeID", signatureJob.nodeID,
				"index", i,
				"signature", hexutil.Bytes(bls.SignatureToBytes(blsSignature)),
			)

			// Add the signature and check if we've reached the requested threshold
			signatureLock.Lock()
			defer signatureLock.Unlock()

			blsSignatures = append(blsSignatures, blsSignature)
			bitSet.Add(i)
			signatureWeight += signatureJob.weight
			log.Info("Updated weight",
				"totalWeight", signatureWeight,
				"addedWeight", signatureJob.weight,
			)

			// If the signature weight meets the requested threshold, cancel signature fetching
			if err := avalancheWarp.VerifyWeight(signatureWeight, totalWeight, a.maxNeededQuorumNum, a.quorumDen); err == nil {
				log.Info("Verify weight passed, exiting aggregation early",
					"maxNeededQuorumNum", a.maxNeededQuorumNum,
					"totalWeight", totalWeight,
					"signatureWeight", signatureWeight,
				)
				signatureFetchCancel()
			}
		}()
	}
	wg.Wait()

	// If I failed to fetch sufficient signature stake, return an error
	if err := avalancheWarp.VerifyWeight(signatureWeight, totalWeight, a.minValidQuorumNum, a.quorumDen); err != nil {
		return nil, fmt.Errorf("failed to aggregate signature: %w", err)
	}

	// Otherwise, return the aggregate signature
	aggregateSignature, err := bls.AggregateSignatures(blsSignatures)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate BLS signatures: %w", err)
	}

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: bitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregateSignature))

	msg, err := avalancheWarp.NewMessage(a.msg, warpSignature)
	if err != nil {
		return nil, fmt.Errorf("failed to construct warp message: %w", err)
	}

	return &AggregateSignatureResult{
		Message:         msg,
		SignatureWeight: signatureWeight,
		TotalWeight:     totalWeight,
	}, nil
}
