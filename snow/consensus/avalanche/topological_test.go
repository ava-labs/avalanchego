// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"testing"
)

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{})
}
