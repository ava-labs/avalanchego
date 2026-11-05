package common

import (
	"encoding/binary"
	"math/rand/v2"
)

func NewRandomSourceFromSeed(seed int64) *rand.Rand {
	var seedBytes [32]byte
	binary.LittleEndian.PutUint64(seedBytes[:8], uint64(seed))
	chacha := rand.NewChaCha8(seedBytes)
	return rand.New(chacha)
}
