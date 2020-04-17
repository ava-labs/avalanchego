// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"
)

func TestTopological(t *testing.T) { ConsensusTest(t, TopologicalFactory{}) }
