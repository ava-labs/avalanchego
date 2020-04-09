// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/validators"
)

// Config wraps the common configurations that are needed by a Snow consensus
// engine
type Config struct {
	Context    *snow.Context
	Validators validators.Set
	Beacons    validators.Set

	Alpha         uint64
	Sender        Sender
	Bootstrapable Bootstrapable
}
