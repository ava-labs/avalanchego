// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestStateObjectPartition(t *testing.T) {
	h1 := common.Hash{}
	NormalizeCoinID(&h1)

	h2 := common.Hash{}
	NormalizeStateKey(&h2)

	if bytes.Equal(h1.Bytes(), h2.Bytes()) {
		t.Fatalf("Expected normalized hashes to be unique")
	}

	h3 := common.Hash([32]byte{0xff})
	NormalizeCoinID(&h3)

	h4 := common.Hash([32]byte{0xff})
	NormalizeStateKey(&h4)

	if bytes.Equal(h3.Bytes(), h4.Bytes()) {
		t.Fatal("Expected normalized hashes to be unqiue")
	}
}
