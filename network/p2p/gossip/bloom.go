// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"
	"encoding/binary"
	"hash"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/ids"
)

var _ hash.Hash64 = (*hasher)(nil)

// NewBloomFilter returns a new instance of a bloom filter with at most
// [maxExpectedElements] elements anticipated at any moment, and a false
// positive probability of [falsePositiveProbability].
func NewBloomFilter(
	maxExpectedElements uint64,
	falsePositiveProbability float64,
) (*BloomFilter, error) {
	bloom, err := bloomfilter.NewOptimal(
		maxExpectedElements,
		falsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	salt, err := randomSalt()
	return &BloomFilter{
		bloom: bloom,
		salt:  salt,
	}, err
}

type BloomFilter struct {
	bloom *bloomfilter.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	h := gossipable.GossipID()
	salted := &hasher{
		hash: h[:],
		salt: b.salt,
	}
	b.bloom.Add(salted)
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	h := gossipable.GossipID()
	salted := &hasher{
		hash: h[:],
		salt: b.salt,
	}
	return b.bloom.Contains(salted)
}

func (b *BloomFilter) Marshal() ([]byte, []byte, error) {
	bloomBytes, err := b.bloom.MarshalBinary()
	// salt must be copied here to ensure the bytes aren't overwritten if salt
	// is later modified.
	salt := b.salt
	return bloomBytes, salt[:], err
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a target false
// positive probability. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	falsePositiveProbability float64,
) (bool, error) {
	if bloomFilter.bloom.FalsePosititveProbability() < falsePositiveProbability {
		return false, nil
	}

	newBloom, err := bloomfilter.New(bloomFilter.bloom.M(), bloomFilter.bloom.K())
	if err != nil {
		return false, err
	}
	salt, err := randomSalt()
	if err != nil {
		return false, err
	}

	bloomFilter.bloom = newBloom
	bloomFilter.salt = salt
	return true, nil
}

func randomSalt() (ids.ID, error) {
	salt := ids.ID{}
	_, err := rand.Read(salt[:])
	return salt, err
}

type hasher struct {
	hash []byte
	salt ids.ID
}

func (h *hasher) Write(p []byte) (n int, err error) {
	h.hash = append(h.hash, p...)
	return len(p), nil
}

func (h *hasher) Sum(b []byte) []byte {
	h.hash = append(h.hash, b...)
	return h.hash
}

func (h *hasher) Reset() {
	h.hash = ids.Empty[:]
}

func (*hasher) BlockSize() int {
	return ids.IDLen
}

func (h *hasher) Sum64() uint64 {
	salted := ids.ID{}
	for i := 0; i < len(h.hash) && i < ids.IDLen; i++ {
		salted[i] = h.hash[i] ^ h.salt[i]
	}

	return binary.BigEndian.Uint64(salted[:])
}

func (h *hasher) Size() int {
	return len(h.hash)
}
