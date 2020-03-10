// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
)

// Config wraps all the parameters needed for an avalanche engine
type Config struct {
	BootstrapConfig

	Params    avalanche.Parameters
	Consensus avalanche.Consensus
}
