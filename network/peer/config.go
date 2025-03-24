// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

type Config struct {
	// Size, in bytes, of the buffer this peer reads messages into
	ReadBufferSize int
	// Size, in bytes, of the buffer this peer writes messages into
	WriteBufferSize int
	Clock           mockable.Clock
	Metrics         *Metrics
	MessageCreator  message.Creator

	Log                  logging.Logger
	InboundMsgThrottler  throttling.InboundMsgThrottler
	Network              Network
	Router               router.InboundHandler
	VersionCompatibility version.Compatibility
	MyNodeID             ids.NodeID
	// MySubnets does not include the primary network ID
	MySubnets          set.Set[ids.ID]
	MySubnetsLock      *sync.RWMutex
	Beacons            validators.Manager
	Validators         validators.Manager
	NetworkID          uint32
	PingFrequency      time.Duration
	PongTimeout        time.Duration
	MaxClockDifference time.Duration

	SupportedACPs []uint32
	ObjectedACPs  []uint32

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	LastSent, LastReceived int64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker tracker.ResourceTracker

	// Calculates uptime of peers
	UptimeCalculator uptime.Calculator

	// Signs my IP so I can send my signed IP address in the Handshake message
	IPSigner *IPSigner

	// IngressConnectionCount counts the ingress (to us) connections.
	IngressConnectionCount atomic.Int64
}
