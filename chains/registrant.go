// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import (
	"github.com/ava-labs/avalanche-go/snow"
)

// Registrant can register the existence of a chain
type Registrant interface {
	RegisterChain(ctx *snow.Context, vm interface{})
}
