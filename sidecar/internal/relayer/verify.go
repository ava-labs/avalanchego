// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"fmt"
	"log"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// QuorumNumerator is the percent of total stake weight required.
const QuorumNumerator = 67

// VerifyAndAggregate verifies each collected signature against the canonical
// validator set, deduplicates signers, and aggregates the valid signatures to
// a quorum BLS aggregate. signerNoun ("attestor"/"validator") is used in the
// per-signer log lines. It returns the signer bitset (in canonical validator
// order), the aggregate signature, the signed weight, and the signed
// percentage of total weight; if quorum is not reached the error is non-nil
// and the aggregate is nil.
func VerifyAndAggregate(
	warpSet validators.WarpSet,
	sigs map[ids.NodeID][]byte,
	unsigned *avalancheWarp.UnsignedMessage,
	signerNoun string,
) (set.Bits, *bls.Signature, uint64, float64, error) {
	signerBits := set.NewBits()
	var validSigs []*bls.Signature
	var signedWeight uint64
	for nodeID, sigBytes := range sigs {
		sig, err := bls.SignatureFromBytes(sigBytes)
		if err != nil {
			log.Printf("%s %s: bad signature: %v", signerNoun, nodeID, err)
			continue
		}
		idx := -1
		for i, vdr := range warpSet.Validators {
			for _, vid := range vdr.NodeIDs {
				if vid == nodeID {
					idx = i
					break
				}
			}
			if idx >= 0 {
				break
			}
		}
		if idx < 0 || !bls.Verify(warpSet.Validators[idx].PublicKey, sig, unsigned.Bytes()) {
			log.Printf("%s %s: not in set or signature invalid — ignoring", signerNoun, nodeID)
			continue
		}
		if signerBits.Contains(idx) {
			continue
		}
		signerBits.Add(idx)
		validSigs = append(validSigs, sig)
		signedWeight += warpSet.Validators[idx].Weight
		log.Printf("%s %s: verified (index %d)", signerNoun, nodeID, idx)
	}
	// big.Int quorum math: primary-network weights at mainnet scale overflow
	// uint64 when multiplied by 100 (see avalancheWarp.VerifyWeight).
	signedTimes100 := new(big.Int).Mul(new(big.Int).SetUint64(signedWeight), big.NewInt(100))
	totalTimesQuorum := new(big.Int).Mul(new(big.Int).SetUint64(warpSet.TotalWeight), big.NewInt(QuorumNumerator))
	pct := float64(signedWeight) / float64(warpSet.TotalWeight) * 100
	if signedTimes100.Cmp(totalTimesQuorum) < 0 {
		return signerBits, nil, signedWeight, pct, fmt.Errorf(
			"quorum not reached: %d/%d weight (%.0f%%)", signedWeight, warpSet.TotalWeight, pct)
	}
	agg, err := bls.AggregateSignatures(validSigs)
	if err != nil {
		return signerBits, nil, signedWeight, pct, fmt.Errorf("aggregate: %w", err)
	}
	return signerBits, agg, signedWeight, pct, nil
}
