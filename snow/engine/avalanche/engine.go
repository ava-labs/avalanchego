// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

// Invariants:
// - A vertex can only be issued if the transactions inside of it can be issued
//   and its parents have been issued.
// - If a vertex or transaction is issued to consensus it can only be removed if
//   it is permanently decided.
//    * If a transition is issued to consensus, each dependent transitions must
//      have been accepted in an earlier epoch, or issued in the same epoch.
// - If a vertex is gossiped from a node, the node must know its full ancestry.
// - If an event is blocking, there must be an outstanding request that could
//   fulfil the event.
// - If a node is queried about a vertex that may have been issued by a correct
//   node, this node must respond with its current preferences.
// - If a transaction is preferred, it should be contained in a vertex that is
//   preferred.
// - When a transition is first issued, it must be preferred in the epoch the
//   node is currently in.

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
