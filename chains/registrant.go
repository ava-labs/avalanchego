// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import "github.com/ava-labs/avalanchego/ids"

// Registrant can register the existence of a chain
type Registrant interface {
	RegisterChain(name string, chainID ids.ID, engine interface{})
}
