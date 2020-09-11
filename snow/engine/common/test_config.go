// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
)

// DefaultConfigTest returns a test configuration
func DefaultConfigTest() Config {
	return Config{
		Ctx:           snow.DefaultContextTest(),
		Validators:    validators.NewSet(),
		Beacons:       validators.NewSet(),
		Sender:        &SenderTest{},
		Bootstrapable: &BootstrapableTest{},
	}
}
