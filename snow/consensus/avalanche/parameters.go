// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"fmt"

	"github.com/chain4travel/caminogo/snow/consensus/snowball"
)

// Parameters the avalanche parameters include the snowball parameters and the
// optimal number of parents
type Parameters struct {
	snowball.Parameters
	Parents   int `json:"parents"`
	BatchSize int `json:"batchSize"`
}

// Valid returns nil if the parameters describe a valid initialization.
func (p Parameters) Valid() error {
	switch {
	case p.Parents <= 1:
		return fmt.Errorf("parents = %d: Fails the condition that: 1 < Parents", p.Parents)
	case p.BatchSize <= 0:
		return fmt.Errorf("batchSize = %d: Fails the condition that: 0 < BatchSize", p.BatchSize)
	default:
		return p.Parameters.Verify()
	}
}
