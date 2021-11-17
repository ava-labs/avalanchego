// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"testing"
)

func TestDirectedConsensus(t *testing.T) { runConsensusTests(t, DirectedFactory{}, "DG") }
