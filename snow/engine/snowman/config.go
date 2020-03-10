// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

// Config wraps all the parameters needed for a snowman engine
type Config struct {
	BootstrapConfig

	Params    snowball.Parameters
	Consensus snowman.Consensus
}
