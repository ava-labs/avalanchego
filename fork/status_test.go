// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestArm(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	otherCfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	before := testForkTime.Add(-time.Minute)
	duringGrace := testForkTime.Add(time.Second)
	after := cfg.SwitchTime().Add(time.Minute)

	tests := []struct {
		name              string
		armAt             *time.Time // if set, Arm once at this time first
		armWith           *fork.Config
		now               time.Time
		cfg               *fork.Config
		wantStartSwitched bool
		wantErr           error
	}{
		{name: "fresh node before T", now: before, cfg: cfg},
		{name: "fresh node after T is a late joiner", now: after, cfg: cfg, wantErr: fork.ErrLateJoiner},
		{name: "armed restart before T", armAt: &before, armWith: cfg, now: before, cfg: cfg},
		{name: "armed restart during grace starts switched", armAt: &before, armWith: cfg, now: duringGrace, cfg: cfg, wantStartSwitched: true},
		{name: "armed restart after switch", armAt: &before, armWith: cfg, now: after, cfg: cfg, wantStartSwitched: true},
		{name: "config changed", armAt: &before, armWith: cfg, now: duringGrace, cfg: otherCfg, wantErr: fork.ErrConfigChanged},
		{name: "re-armed before T with a changed config", armAt: &before, armWith: cfg, now: before, cfg: otherCfg},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := memdb.New()
			if tt.armAt != nil {
				_, err := fork.Arm(db, tt.armWith, *tt.armAt)
				require.NoError(t, err, "first Arm()")
			}
			startSwitched, err := fork.Arm(db, tt.cfg, tt.now)
			require.ErrorIs(t, err, tt.wantErr, "Arm()")
			require.Equal(t, tt.wantStartSwitched, startSwitched, "Arm() startSwitched")
		})
	}

	t.Run("re-arm before T replaces the armed config", func(t *testing.T) {
		db := memdb.New()
		_, err := fork.Arm(db, cfg, before)
		require.NoError(t, err, "Arm(cfg, before)")
		_, err = fork.Arm(db, otherCfg, before)
		require.NoError(t, err, "Arm(otherCfg, before)")

		startSwitched, err := fork.Arm(db, otherCfg, duringGrace)
		require.NoError(t, err, "Arm(otherCfg, duringGrace)")
		require.True(t, startSwitched, "Arm(otherCfg, duringGrace) startSwitched")

		_, err = fork.Arm(db, cfg, duringGrace)
		require.ErrorIs(t, err, fork.ErrConfigChanged, "Arm(cfg, duringGrace)")
	})
}

func TestStatusPhase(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	now := testForkTime.Add(-time.Second)
	switched := false
	s := fork.NewStatus(cfg, func() time.Time { return now }, func() bool { return switched })

	require.Equal(t, fork.Observing, s.Phase(), "Phase() before T")
	now = testForkTime
	require.Equal(t, fork.Grace, s.Phase(), "Phase() at T")
	switched = true
	require.Equal(t, fork.Switched, s.Phase(), "Phase() after switch")
	require.Equal(t, "switched", s.Phase().String(), "Phase().String()")
}

func TestStatusHealthCheck(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	now := cfg.SwitchTime().Add(11 * time.Minute)
	switched := false
	s := fork.NewStatus(cfg, func() time.Time { return now }, func() bool { return switched })

	_, err := s.HealthCheck(t.Context())
	require.NoError(t, err, "HealthCheck() before switch")

	switched = true
	_, err = s.HealthCheck(t.Context())
	require.ErrorIs(t, err, fork.ErrForkHeightUnknown, "HealthCheck() switched without H_fork")

	now = cfg.SwitchTime().Add(time.Minute)
	_, err = s.HealthCheck(t.Context())
	require.NoError(t, err, "HealthCheck() switched recently without H_fork")

	s.SetForkPoint(constants.PlatformChainID, fork.ForkPoint{BlockID: ids.GenerateTestID(), Height: 41})
	chainID := ids.GenerateTestID()
	point := fork.ForkPoint{BlockID: ids.GenerateTestID(), Height: 7}
	s.SetForkPoint(chainID, point)
	details, err := s.HealthCheck(t.Context())
	require.NoError(t, err, "HealthCheck() with H_fork")

	report, ok := details.(fork.Report)
	require.True(t, ok, "HealthCheck() details type")
	require.Equal(t, "switched", report.Phase, "Report.Phase")
	require.NotNil(t, report.ForkHeight, "Report.ForkHeight")
	require.Equal(t, uint64(42), *report.ForkHeight, "Report.ForkHeight")
	require.Len(t, report.ForkPoints, 2, "Report.ForkPoints")
	require.Equal(t, point, report.ForkPoints[chainID.String()], "Report.ForkPoints[chain]")
}

func TestForkPointBytesRoundTrip(t *testing.T) {
	want := fork.ForkPoint{BlockID: ids.GenerateTestID(), Height: 1234}
	b, err := want.Bytes()
	require.NoError(t, err, "Bytes()")
	got, err := fork.ParseForkPoint(b)
	require.NoError(t, err, "ParseForkPoint()")
	require.Equal(t, want, got, "ParseForkPoint()")
}
