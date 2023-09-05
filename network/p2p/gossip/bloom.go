// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
		Bloom: bloom,
		Salt:  salt,
	}, err
}

type BloomFilter struct {
	Bloom *bloomfilter.Filter
	// Salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	Salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	h := gossipable.GetID()
	salted := &hasher{
		hash: h[:],
		salt: b.Salt,
	}
	b.Bloom.Add(salted)
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	h := gossipable.GetID()
	salted := &hasher{
		hash: h[:],
		salt: b.Salt,
	}
	return b.Bloom.Contains(salted)
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a target false
// positive probability. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	falsePositiveProbability float64,
) (bool, error) {
	if bloomFilter.Bloom.FalsePosititveProbability() < falsePositiveProbability {
		return false, nil
	}

	newBloom, err := bloomfilter.New(bloomFilter.Bloom.M(), bloomFilter.Bloom.K())
	if err != nil {
		return false, err
	}
	salt, err := randomSalt()
	if err != nil {
		return false, err
	}

	bloomFilter.Bloom = newBloom
	bloomFilter.Salt = salt
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
