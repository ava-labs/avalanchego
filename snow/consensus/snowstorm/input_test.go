// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestInputConsensus(t *testing.T) { runConsensusTests(t, inputFactory{}, "IG") }
