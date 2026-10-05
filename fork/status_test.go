// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/fork/forktest"
	"github.com/ava-labs/avalanchego/ids"
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
	s := fork.NewStatus(cfg, func() time.Time { return now })

	require.Equal(t, fork.Observing, s.Phase(), "Phase() before T")
	now = testForkTime
	require.Equal(t, fork.Grace, s.Phase(), "Phase() at T")
	s.MarkSwitched()
	require.Equal(t, fork.Switched, s.Phase(), "Phase() after MarkSwitched()")
	require.Equal(t, "switched", s.Phase().String(), "Phase().String()")
}

func TestStatusHealthCheck(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	now := cfg.SwitchTime().Add(11 * time.Minute)
	s := fork.NewStatus(cfg, func() time.Time { return now })

	_, err := s.HealthCheck(t.Context())
	require.NoError(t, err, "HealthCheck() before switch")

	s.MarkSwitched()
	_, err = s.HealthCheck(t.Context())
	require.ErrorIs(t, err, fork.ErrForkHeightUnknown, "HealthCheck() switched without H_fork")

	now = cfg.SwitchTime().Add(time.Minute)
	_, err = s.HealthCheck(t.Context())
	require.NoError(t, err, "HealthCheck() switched recently without H_fork")

	s.SetForkHeight(42)
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
	require.Equal(t, map[string]fork.ForkPoint{chainID.String(): point}, report.ForkPoints, "Report.ForkPoints")
}

func TestForkPointBytesRoundTrip(t *testing.T) {
	want := fork.ForkPoint{BlockID: ids.GenerateTestID(), Height: 1234}
	got, err := fork.ParseForkPoint(want.Bytes())
	require.NoError(t, err, "ParseForkPoint()")
	require.Equal(t, want, got, "ParseForkPoint()")

	_, err = fork.ParseForkPoint([]byte{1, 2, 3})
	require.ErrorIs(t, err, fork.ErrInvalidForkPoint, "ParseForkPoint(short)")
}

func TestStatusRegisterMetrics(t *testing.T) {
	cfg := forktest.NewConfig(t, testForkTime, ids.GenerateTestNodeID())
	s := fork.NewStatus(cfg, func() time.Time { return testForkTime.Add(-time.Second) })
	reg := prometheus.NewRegistry()
	require.NoError(t, s.RegisterMetrics(reg), "RegisterMetrics()")

	families, err := reg.Gather()
	require.NoError(t, err, "Gather()")
	require.Len(t, families, 1, "Gather()")
	require.Equal(t, "phase", families[0].GetName(), "metric name")
	require.Zero(t, families[0].GetMetric()[0].GetGauge().GetValue(), "phase gauge while observing")
}
