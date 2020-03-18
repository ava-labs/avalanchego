// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

// ChainType ...
type ChainType int

// Chain types
const (
	unknown ChainType = iota
	spChain
	spDAG
	avmDAG
)
