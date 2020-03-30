// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/vms/components/codec"
)

// VM that this Fx must be run by
type VM interface {
	Codec() codec.Codec
	Clock() *timer.Clock
	Logger() logging.Logger
}
