// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"encoding/binary"
	"hash"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"
	"golang.org/x/exp/rand"
)

const hashLength = 32

var _ hash.Hash64 = (*hasher)(nil)

type Hash [hashLength]byte

// NewBloomFilter returns a new instance of a bloom filter with at most
// [maxExpectedElements] elements anticipated at any moment, and a collision
// rate of [falsePositiveProbability].
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

	bloomFilter := &BloomFilter{
		Bloom: bloom,
		Salt:  randomSalt(),
	}
	return bloomFilter, nil
}

type BloomFilter struct {
	Bloom *bloomfilter.Filter
	// Salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	Salt Hash
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	h := gossipable.GetHash()
	salted := &hasher{
		hash: h[:],
		salt: b.Salt,
	}
	b.Bloom.Add(salted)
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	h := gossipable.GetHash()
	salted := &hasher{
		hash: h[:],
		salt: b.Salt,
	}
	return b.Bloom.Contains(salted)
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a ratio of
// filled elements. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	maxFilledRatio float64,
) bool {
	if bloomFilter.Bloom.PreciseFilledRatio() < maxFilledRatio {
		return false
	}

	// it's not possible for this to error assuming that the original
	// bloom filter's parameters were valid
	fresh, _ := bloomfilter.New(bloomFilter.Bloom.M(), bloomFilter.Bloom.K())
	bloomFilter.Bloom = fresh
	bloomFilter.Salt = randomSalt()
	return true
}

func randomSalt() Hash {
	salt := Hash{}
	r := rand.New(rand.NewSource(uint64(time.Now().Nanosecond())))
	_, _ = r.Read(salt[:])
	return salt
}

var _ hash.Hash64 = (*hasher)(nil)

type hasher struct {
	hash []byte
	salt Hash
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
	reset := Hash{}
	h.hash = reset[:]
}

func (*hasher) BlockSize() int {
	return hashLength
}

func (h *hasher) Sum64() uint64 {
	salted := Hash{}
	for i := 0; i < len(h.hash) && i < hashLength; i++ {
		salted[i] = h.hash[i] ^ h.salt[i]
	}

	return binary.BigEndian.Uint64(salted[:])
}

func (h *hasher) Size() int {
	return len(h.hash)
}
