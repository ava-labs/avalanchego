// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/MetalBlockchain/metalgo/snow/consensus/snowball"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{factory: snowball.SnowflakeFactory})
}
