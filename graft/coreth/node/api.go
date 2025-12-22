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
// Copyright 2015 The go-ethereum Authors
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

package node

import (
	"github.com/ava-labs/avalanchego/graft/coreth/internal/debug"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/crypto"
)

// apis returns the collection of built-in RPC APIs.
func (n *Node) apis() []rpc.API {
	return []rpc.API{
		{
			Namespace: "debug",
			Service:   debug.Handler,
			Name:      "debug-handler",
		},
		{
			Namespace: "web3",
			Service:   &web3API{n},
			Name:      "web3",
		},
	}
}

// web3API offers helper utils
type web3API struct {
	stack *Node
}

// ClientVersion returns the node name
func (s *web3API) ClientVersion() string {
	return s.stack.config.CorethVersion
}

// Sha3 applies the ethereum sha3 implementation on the input.
// It assumes the input is hex encoded.
func (s *web3API) Sha3(input hexutil.Bytes) hexutil.Bytes {
	return crypto.Keccak256(input)
}
