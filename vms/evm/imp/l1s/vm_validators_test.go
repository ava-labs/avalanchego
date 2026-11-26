// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"fmt"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"

	l1api "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api"
	saerpc "github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// TestUptimeAPI asserts that validator_uptime MUST report the uptime tracked
// from [VM.Connected] and [VM.Disconnected], and the time it was last updated.
func TestUptimeAPI(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	validationID := ids.GenerateTestID()
	ctx, sut := newSUT(t, withCurrentValidator(validationID, nodeID, uint64(testStartTime.Unix()))) //#nosec G115 -- Known non-negative

	// The disconnected hour separates uptime from elapsed time.
	require.NoErrorf(t, sut.Connected(ctx, nodeID, version.Current), "%T.Connected()", sut.VM)
	sut.clock.Advance(time.Hour)
	require.NoErrorf(t, sut.Disconnected(ctx, nodeID), "%T.Disconnected()", sut.VM)
	sut.clock.Advance(time.Hour)

	got, err := sut.l1sClient.Uptime(ctx, validationID)
	require.NoErrorf(t, err, "%T.Uptime()", sut.l1sClient)
	want := &l1api.Uptime{
		Seconds:     hexutil.Uint64(time.Hour.Seconds()),
		LastUpdated: hexutil.Uint64(testStartTime.Add(2 * time.Hour).Unix()), //#nosec G115 -- Known non-negative
	}
	require.Equalf(t, want, got, "%T.Uptime()", sut.l1sClient)
}

// rpcErrorCode returns a [testerr.Want] matching a JSON-RPC error with code.
func rpcErrorCode(code int) testerr.Want {
	return testerr.As(func(got rpc.Error) string {
		if got.ErrorCode() == code {
			return ""
		}
		return fmt.Sprintf("JSON-RPC error code %d", code)
	})
}

// TestUptimeAPIErrors asserts that validator_uptime MUST return the JSON-RPC
// error for each failure.
func TestUptimeAPIErrors(t *testing.T) {
	tests := []struct {
		name    string
		opts    []sutOption
		wantErr testerr.Want
	}{
		{
			name: "unknown_validation_id",
			wantErr: testerr.AllOf(
				rpcErrorCode(-32000), // JSON-RPC server error
				testerr.Contains(uptimetracker.ErrValidationIDNotFound.Error()),
			),
		},
		{
			name: "api_disabled",
			opts: []sutOption{withAPIs(saerpc.DefaultAPIs())},
			wantErr: testerr.AllOf(
				rpcErrorCode(-32601), // JSON-RPC method not found
				testerr.Contains("does not exist/is not available"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, sut := newSUT(t, tt.opts...)

			_, err := sut.l1sClient.Uptime(ctx, ids.GenerateTestID())
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.Uptime() error (-want +got)\n%s", sut.l1sClient, diff)
			}
		})
	}
}
