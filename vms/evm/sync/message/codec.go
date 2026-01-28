// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	Version        = uint16(0)
	maxMessageSize = 2*units.MiB - 64*units.KiB // Subtract 64 KiB from p2p network cap to leave room for encoding overhead from AvalancheGo
)

var (
	codecOnce sync.Once
	manager   codec.Manager
)

// Codec returns the codec manager for this package, initializing it lazily on first access.
// This avoids using init() and initializes the codec only when needed.
// NOTE: Panics if codec initialization fails (e.g., duplicate type registration).
// Such errors indicate programming bugs and should never occur at runtime.
func Codec() codec.Manager {
	codecOnce.Do(func() {
		c := codec.NewManager(maxMessageSize)
		lc := linearcodec.NewDefault()

		errs := wrappers.Errs{}
		// Gossip types and sync summary type removed from codec
		lc.SkipRegistrations(3)
		errs.Add(
			// state sync types
			lc.RegisterType(BlockRequest{}),
			lc.RegisterType(BlockResponse{}),
			lc.RegisterType(LeafsRequest{}),
			lc.RegisterType(LeafsResponse{}),
			lc.RegisterType(CodeRequest{}),
			lc.RegisterType(CodeResponse{}),
		)

		// Deprecated Warp request/response types are skipped
		// See https://github.com/ava-labs/coreth/pull/999
		lc.SkipRegistrations(3)

		errs.Add(c.RegisterCodec(Version, lc))

		if errs.Errored() {
			panic(fmt.Errorf("failed to initialize message codec: %w", errs.Err))
		}
		manager = c
	})
	return manager
}
