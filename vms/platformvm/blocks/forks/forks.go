// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package forks

type Fork uint16

// These are the codec versions we use.
const (
	// This codec version marshals/unmarshals Apricot blocks.
	Apricot Fork = 0
	// This codec version marshals/unmarshals Blueberry blocks
	// and Apricot Blocks.
	Blueberry Fork = 1
)

func (f Fork) String() string {
	switch f {
	case Apricot:
		return "Apricot fork"
	case Blueberry:
		return "Blueberry fork"
	default:
		return "Unknown fork"
	}
}
