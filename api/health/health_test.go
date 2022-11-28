// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	checkFreq = time.Millisecond
	awaitFreq = 50 * time.Microsecond
)

func awaitReadiness(r Reporter) {
	for {
		_, ready := r.Readiness()
		if ready {
			return
		}
		time.Sleep(awaitFreq)
	}
}

func awaitHealthy(r Reporter, healthy bool) {
	for {
		_, ok := r.Health()
		if ok == healthy {
			return
		}
		time.Sleep(awaitFreq)
	}
}

func awaitLiveness(r Reporter, liveness bool) {
	for {
		_, ok := r.Liveness()
		if ok == liveness {
			return
		}
		time.Sleep(awaitFreq)
	}
}

func TestDuplicatedRegistations(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	require.NoError(err)
	err = h.RegisterReadinessCheck("check", check)
	require.ErrorIs(err, errDuplicateCheck)

	err = h.RegisterHealthCheck("check", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	require.ErrorIs(err, errDuplicateCheck)

	err = h.RegisterLivenessCheck("check", check)
	require.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	require.ErrorIs(err, errDuplicateCheck)
}

func TestDefaultFailing(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	{
		err = h.RegisterReadinessCheck("check", check)
		require.NoError(err)

		readinessResult, readiness := h.Readiness()
		require.Len(readinessResult, 1)
		require.Contains(readinessResult, "check")
		require.Equal(notYetRunResult, readinessResult["check"])
		require.False(readiness)
	}

	{
		err = h.RegisterHealthCheck("check", check)
		require.NoError(err)

		healthResult, health := h.Health()
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check")
		require.Equal(notYetRunResult, healthResult["check"])
		require.False(health)
	}

	{
		err = h.RegisterLivenessCheck("check", check)
		require.NoError(err)

		livenessResult, liveness := h.Liveness()
		require.Len(livenessResult, 1)
		require.Contains(livenessResult, "check")
		require.Equal(notYetRunResult, livenessResult["check"])
		require.False(liveness)
	}
}

func TestPassingChecks(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	require.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	require.NoError(err)

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	{
		awaitReadiness(h)

		readinessResult, readiness := h.Readiness()
		require.Len(readinessResult, 1)
		require.Contains(readinessResult, "check")

		result := readinessResult["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(readiness)
	}

	{
		awaitHealthy(h, true)

		healthResult, health := h.Health()
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check")

		result := healthResult["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(health)
	}

	{
		awaitLiveness(h, true)

		livenessResult, liveness := h.Liveness()
		require.Len(livenessResult, 1)
		require.Contains(livenessResult, "check")

		result := livenessResult["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(liveness)
	}
}

func TestPassingThenFailingChecks(t *testing.T) {
	require := require.New(t)

	var (
		shouldCheckErr utils.AtomicBool
		checkErr       = errors.New("unhealthy")
	)
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		if shouldCheckErr.GetValue() {
			return checkErr.Error(), checkErr
		}
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	require.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	require.NoError(err)

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitReadiness(h)
	awaitHealthy(h, true)
	awaitLiveness(h, true)

	{
		_, readiness := h.Readiness()
		require.True(readiness)

		_, health := h.Health()
		require.True(health)

		_, liveness := h.Liveness()
		require.True(liveness)
	}

	shouldCheckErr.SetValue(true)

	awaitHealthy(h, false)
	awaitLiveness(h, false)

	{
		// Notice that Readiness is a monotonic check - so it still reports
		// ready.
		_, readiness := h.Readiness()
		require.True(readiness)

		_, health := h.Health()
		require.False(health)

		_, liveness := h.Liveness()
		require.False(liveness)
	}
}

func TestDeadlockRegression(t *testing.T) {
	require := require.New(t)

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	var lock sync.Mutex
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		lock.Lock()
		time.Sleep(time.Nanosecond)
		lock.Unlock()
		return "", nil
	})

	h.Start(context.Background(), time.Nanosecond)
	defer h.Stop()

	for i := 0; i < 1000; i++ {
		lock.Lock()
		err = h.RegisterHealthCheck(fmt.Sprintf("check-%d", i), check)
		lock.Unlock()
		require.NoError(err)
	}

	awaitHealthy(h, true)
}
