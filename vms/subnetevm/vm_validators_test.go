// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/uptimetracker"

	subnetevmapi "github.com/ava-labs/avalanchego/vms/subnetevm/api"
)

// getCurrentValidators issues a `validators.getCurrentValidators` JSON-RPC
// call via the SUT's typed validators client.
func (s *SUT) getCurrentValidators(t *testing.T, nodeIDs []ids.NodeID) []subnetevmapi.CurrentValidator {
	t.Helper()
	validators, err := s.client.GetCurrentValidators(s.ctx, nodeIDs)
	require.NoError(t, err)
	return validators
}

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
		startTime        = uint64(baseTime.Unix())
		now              = baseTime
	)

	sut := newSUT(
		t,
		withFork(upgradetest.Latest),
		withNow(now),
		withCurrentValidatorSet(testValidationID, testNodeID, startTime),
	)

	// (0) Validator known but not yet connected: 0 uptime, lastUpdated == baseTime.
	upDuration, lastUpdated, err := sut.vm.validators.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, time.Duration(0), upDuration, "initial uptime must be 0")
	require.Equalf(t, baseTime, lastUpdated, "initial lastUpdated must be baseTime")

	// (1) Connect and let 1h pass.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 1*time.Hour)

	upDuration, lastUpdated, err = sut.vm.validators.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, 1*time.Hour, upDuration, "uptime after 1h connected must be 1h")
	require.Equalf(t, baseTime.Add(1*time.Hour), lastUpdated, "lastUpdated must reflect new clock time")

	// (2) Disconnect and let another hour pass: uptime must NOT advance,
	//     but lastUpdated must.
	require.NoError(t, sut.vm.Disconnected(sut.ctx, testNodeID))
	sut.advanceTime(t, 1*time.Hour)

	upDuration, lastUpdated, err = sut.vm.validators.GetUptime(testValidationID)
	require.NoError(t, err)
	require.Equalf(t, 1*time.Hour, upDuration, "uptime must NOT advance while disconnected")
	require.Equalf(t, baseTime.Add(2*time.Hour), lastUpdated, "lastUpdated must still advance")

	// (3) Reconnect and let 30m pass: uptime must reach 1h30m.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 30*time.Minute)

	upDuration, lastUpdated, err = sut.vm.validators.GetUptime(testValidationID)
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
	sut := newSUT(t, withFork(upgradetest.Latest))

	_, _, err := sut.vm.validators.GetUptime(ids.GenerateTestID())
	require.ErrorIs(t, err, uptimetracker.ErrValidationIDNotFound)
}

// TestGetCurrentValidatorsAPI exercises the `validators.getCurrentValidators`
// JSON-RPC handler end-to-end:
//   - Unfiltered query returns the configured validator set (one entry).
//   - `IsConnected` toggles after `vm.Connected/Disconnected`.
//   - `UptimeSeconds`/`UptimePercentage` reflect the faked clock.
//   - A `NodeIDs` filter that excludes the only validator returns an empty
//     list.
func TestGetCurrentValidatorsAPI(t *testing.T) {
	var (
		testNodeID       = ids.GenerateTestNodeID()
		testValidationID = ids.GenerateTestID()
		baseTime         = time.Unix(0, 0)
		startTime        = uint64(baseTime.Unix())
		now              = baseTime
	)

	sut := newSUT(
		t,
		withFork(upgradetest.Latest),
		withNow(now),
		withCurrentValidatorSet(testValidationID, testNodeID, startTime),
	)

	// (a) Unfiltered, pre-connect: one entry, IsConnected=false.
	got := sut.getCurrentValidators(t, nil)
	require.Lenf(t, got, 1, "expected exactly one current validator, got %d", len(got))
	v := got[0]
	require.Equal(t, testValidationID, v.ValidationID)
	require.Equal(t, testNodeID, v.NodeID)
	require.Equal(t, startTime, v.StartTimestamp)
	require.Falsef(t, v.IsConnected, "validator should not be reported as connected before vm.Connected")

	// (b) Connect + advance clock 1h: IsConnected=true, uptime 1h.
	require.NoError(t, sut.vm.Connected(sut.ctx, testNodeID, version.Current))
	sut.advanceTime(t, 1*time.Hour)

	got = sut.getCurrentValidators(t, nil)
	require.Len(t, got, 1)
	v = got[0]
	require.Truef(t, v.IsConnected, "validator should be reported as connected after vm.Connected")
	require.Equalf(t, uint64(3600), v.UptimeSeconds, "uptime should be 1h (3600s)")
	require.Equalf(t, uint64(100), uint64(v.UptimePercentage),
		"uptime percentage should be 100%% (was connected for the entire window)")

	// (c) Disconnect + advance another hour: IsConnected=false; uptime
	//     percentage drops to ~50% (1h connected out of 2h tracked).
	require.NoError(t, sut.vm.Disconnected(sut.ctx, testNodeID))
	sut.advanceTime(t, 1*time.Hour)

	got = sut.getCurrentValidators(t, nil)
	require.Len(t, got, 1)
	v = got[0]
	require.Falsef(t, v.IsConnected, "validator should not be reported as connected after vm.Disconnected")
	require.Equalf(t, uint64(3600), v.UptimeSeconds, "uptime must NOT advance while disconnected")
	require.Equalf(t, uint64(50), uint64(v.UptimePercentage),
		"uptime percentage should be 50%% (1h connected out of 2h tracked)")

	// (d) Filter that excludes the only validator: empty result.
	other := ids.GenerateTestNodeID()
	got = sut.getCurrentValidators(t, []ids.NodeID{other})
	require.Emptyf(t, got, "filter excluding the only validator should return an empty list")

	// (e) Filter that includes the only validator: still one entry.
	got = sut.getCurrentValidators(t, []ids.NodeID{testNodeID})
	require.Len(t, got, 1)
	require.Equal(t, testNodeID, got[0].NodeID)
}
