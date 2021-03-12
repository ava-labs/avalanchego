// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

// Registrant can register the existence of a chain
type Registrant interface {
	RegisterChain(name string, engine interface{})
}
