// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/uptimetracker"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/version"

	l1api "github.com/ava-labs/avalanchego/vms/evm/imp/l1s/api"
	saerpc "github.com/ava-labs/avalanchego/vms/evm/sae/rpc"
)

// TestUptimeTracker exercises the SAE-side uptime tracker through
// `vm.Connected/Disconnected` and a faked `mockable.Clock`.
//
//  0. After `SetState(NormalOp)` the validator is registered with 0 uptime.
//  1. After being connected for 1 hour, uptime is 1 hour.
//  2. After being disconnected for 2 hours, uptime is still 1 hour but
//     `lastUpdated` advances.
//  3. After being reconnected for another 30 minutes, uptime is 1h30m.
func TestUptimeTracker(t *testing.T) {
	var (
		testNodeID       = ids.GenerateTestNodeID()
		testValidationID = ids.GenerateTestID()
		baseTime         = time.Unix(0, 0)
		startTime        = uint64(baseTime.Unix()) // #nosec G115 -- known positive test timestamp
		now              = baseTime
	)

	sut := newL1SUT(
		t,
		withFork(upgradetest.Latest),
		withNow(now),
		withCurrentValidatorSet(testValidationID, testNodeID, startTime),
	)

	// (0) Validator known but not yet connected: 0 uptime, lastUpdated == baseTime.
	upDuration, lastUpdated, err := sut.vm.uptime.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, time.Duration(0), upDuration, "initial uptime must be 0")
	require.Equalf(t, baseTime, lastUpdated, "initial lastUpdated must be baseTime")

	// (1) Connect and let 1h pass.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 1*time.Hour)

	upDuration, lastUpdated, err = sut.vm.uptime.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, 1*time.Hour, upDuration, "uptime after 1h connected must be 1h")
	require.Equalf(t, baseTime.Add(1*time.Hour), lastUpdated, "lastUpdated must reflect new clock time")

	// (2) Disconnect and let another hour pass: uptime must NOT advance,
	//     but lastUpdated must.
	require.NoError(t, sut.vm.Disconnected(sut.ctx, testNodeID))
	sut.advanceTime(t, 1*time.Hour)

	upDuration, lastUpdated, err = sut.vm.uptime.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, 1*time.Hour, upDuration, "uptime must NOT advance while disconnected")
	require.Equalf(t, baseTime.Add(2*time.Hour), lastUpdated, "lastUpdated must still advance")

	// (3) Reconnect and let 30m pass: uptime must reach 1h30m.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 30*time.Minute)

	upDuration, lastUpdated, err = sut.vm.uptime.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, 1*time.Hour+30*time.Minute, upDuration,
		"uptime must be 1h30m (1h initial + 30m reconnect)")
	require.Equalf(t, baseTime.Add(2*time.Hour+30*time.Minute), lastUpdated,
		"lastUpdated must reflect final clock time")
}

// TestUptimeTrackerUnknownValidationID asserts that querying uptime for a
// validation ID the tracker has never seen returns
// `uptimetracker.ErrValidationIDNotFound`.
func TestUptimeTrackerUnknownValidationID(t *testing.T) {
	sut := newL1SUT(t, withFork(upgradetest.Latest))

	_, _, err := sut.vm.uptime.GetUptime(ids.GenerateTestID())
	require.ErrorIs(t, err, uptimetracker.ErrValidationIDNotFound)
}

// TestUptimeAPI asserts that `validator_uptime` reports the tracked uptime and
// its last-updated time, and errors for an untracked validation ID.
func TestUptimeAPI(t *testing.T) {
	var (
		testNodeID       = ids.GenerateTestNodeID()
		testValidationID = ids.GenerateTestID()
		baseTime         = time.Unix(0, 0)
		startTime        = uint64(baseTime.Unix()) // #nosec G115 -- known positive test timestamp
	)

	sut := newL1SUT(
		t,
		withFork(upgradetest.Latest),
		withNow(baseTime),
		withCurrentValidatorSet(testValidationID, testNodeID, startTime),
	)

	// Connected for 1h then disconnected for 1h, so the uptime and
	// last-updated time differ.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 1*time.Hour)
	require.NoError(t, sut.vm.Disconnected(sut.ctx, testNodeID))
	sut.advanceTime(t, 1*time.Hour)

	got, err := sut.client.Uptime(sut.ctx, testValidationID)
	require.NoError(t, err)
	want := &l1api.Uptime{
		Seconds:     hexutil.Uint64((1 * time.Hour).Seconds()),
		LastUpdated: hexutil.Uint64(baseTime.Add(2 * time.Hour).Unix()),
	}
	require.Equal(t, want, got)

	_, err = sut.client.Uptime(sut.ctx, ids.GenerateTestID())
	require.ErrorContains(t, err, uptimetracker.ErrValidationIDNotFound.Error()) //nolint:forbidigo // RPC error
}

// TestValidatorAPIDisabled asserts that the validator namespace MUST NOT be
// served unless [apiValidator] is enabled.
func TestValidatorAPIDisabled(t *testing.T) {
	sut := newL1SUT(t, withAPIs(saerpc.DefaultAPIs()))

	_, err := sut.client.Uptime(sut.ctx, ids.GenerateTestID())
	var rpcErr rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	const methodNotFound = -32601 // JSON-RPC 2.0
	require.Equal(t, methodNotFound, rpcErr.ErrorCode(), "%T.Uptime() error code", sut.client)
}
