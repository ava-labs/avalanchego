// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
)

// reserved is every [common.AppError] a sync handler can return without
// declaring it, so a per-RPC sentinel must avoid all of them.
var reserved = []*common.AppError{
	p2p.ErrUnexpected,
	p2p.ErrUnregisteredHandler,
	p2p.ErrNotValidator,
	p2p.ErrThrottled,
	common.ErrUndefined,
	common.ErrTimeout,
	handlers.ErrMalformedRequest,
	handlers.ErrMarshalResponse,
}

// RequireDistinctAppErrors asserts each sentinel is identifiable by its code,
// that the code is positive, and that it collides with neither the p2p
// framework nor the handler shell.
//
// [common.AppError.Is] compares Code and nothing else, so a shared code makes
// two sentinels the same error whatever their messages say.
func RequireDistinctAppErrors(tb testing.TB, sentinels map[string]*common.AppError) {
	tb.Helper()

	seen := make(map[int32]string, len(sentinels))
	for name, sentinel := range sentinels {
		require.ErrorIsf(tb, sentinel, &common.AppError{Code: sentinel.Code},
			"%s is not matchable by its code", name)
		require.Positivef(tb, sentinel.Code,
			"%s needs a positive code, p2p and the engine own the rest", name)

		for _, r := range reserved {
			// The shell checking its own sentinels passes them in here.
			if sentinel == r {
				continue
			}
			require.NotErrorIsf(tb, sentinel, r, "%s collides with %q", name, r.Message)
		}

		other, dup := seen[sentinel.Code]
		require.Falsef(tb, dup, "%s and %s share code %d", name, other, sentinel.Code)
		seen[sentinel.Code] = name
	}
}
