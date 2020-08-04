// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/gecko/ids"
)

// Validator is the minimal description of someone that can be sampled.
type Validator interface {
	// ID returns the unique id of this validator
	ID() ids.ShortID

	// Weight that can be used for weighted sampling.
	// If this validator is validating the default subnet, returns the amount of $AVAX staked
	Weight() uint64
}
