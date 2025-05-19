// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	"bytes"
	"testing"

	"github.com/ava-labs/libevm/common"
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
