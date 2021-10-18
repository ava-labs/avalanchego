// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"time"

	"github.com/ava-labs/avalanchego/message"
)

type messageWrap struct {
	inMsg    message.InboundMessage // Must always be set
	deadline time.Time              // Time this message must be responded to
}
