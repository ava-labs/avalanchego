// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import "testing"

func TestTopological(t *testing.T) {
	runConsensusTests(t, TopologicalFactory{})
}
