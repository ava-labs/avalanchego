// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Engine describes the events that can occur on a consensus instance
type Engine interface {
	common.Engine

	/*
	 ***************************************************************************
	 ***************************** Setup/Teardown ******************************
	 ***************************************************************************
	 */

	// Initialize this engine.
	Initialize(Config)
}
