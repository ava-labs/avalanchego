// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	checkFreq    = time.Millisecond
	awaitFreq    = 50 * time.Microsecond
	awaitTimeout = 30 * time.Second
)

var errUnhealthy = errors.New("unhealthy")

func awaitReadiness(t *testing.T, r Reporter, ready bool) {
	require.Eventually(t, func() bool {
		_, ok := r.Readiness()
		return ok == ready
	}, awaitTimeout, awaitFreq)
}

func awaitHealthy(t *testing.T, r Reporter, healthy bool) {
	require.Eventually(t, func() bool {
		_, ok := r.Health()
		return ok == healthy
	}, awaitTimeout, awaitFreq)
}

func awaitLiveness(t *testing.T, r Reporter, liveness bool) {
	require.Eventually(t, func() bool {
		_, ok := r.Liveness()
		return ok == liveness
	}, awaitTimeout, awaitFreq)
}

func TestDuplicatedRegistrations(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	require.NoError(h.RegisterReadinessCheck("check", check))
	err = h.RegisterReadinessCheck("check", check)
	require.ErrorIs(err, errDuplicateCheck)

	require.NoError(h.RegisterHealthCheck("check", check))
	err = h.RegisterHealthCheck("check", check)
	require.ErrorIs(err, errDuplicateCheck)

	require.NoError(h.RegisterLivenessCheck("check", check))
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
		require.NoError(h.RegisterReadinessCheck("check", check))

		readinessResult, readiness := h.Readiness()
		require.Len(readinessResult, 1)
		require.Contains(readinessResult, "check")
		require.Equal(notYetRunResult, readinessResult["check"])
		require.False(readiness)
	}

	{
		require.NoError(h.RegisterHealthCheck("check", check))

		healthResult, health := h.Health()
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check")
		require.Equal(notYetRunResult, healthResult["check"])
		require.False(health)
	}

	{
		require.NoError(h.RegisterLivenessCheck("check", check))

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

	require.NoError(h.RegisterReadinessCheck("check", check))
	require.NoError(h.RegisterHealthCheck("check", check))
	require.NoError(h.RegisterLivenessCheck("check", check))

	h.Start(t.Context(), checkFreq)
	defer h.Stop()

	{
		awaitReadiness(t, h, true)

		readinessResult, readiness := h.Readiness()
		require.Len(readinessResult, 1)
		require.Contains(readinessResult, "check")

		result := readinessResult["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(readiness)
	}

	{
		awaitHealthy(t, h, true)

		healthResult, health := h.Health()
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check")

		result := healthResult["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(health)
	}

	{
		awaitLiveness(t, h, true)

		livenessResult, liveness := h.Liveness()
		require.Len(livenessResult, 1)
		require.Contains(livenessResult, "check")

		result := livenessResult["check"]
		require.Empty(result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(liveness)
	}
}

func TestPassingThenFailingChecks(t *testing.T) {
	require := require.New(t)

	var shouldCheckErr utils.Atomic[bool]
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		if shouldCheckErr.Get() {
			return errUnhealthy.Error(), errUnhealthy
		}
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	require.NoError(h.RegisterReadinessCheck("check", check))
	require.NoError(h.RegisterHealthCheck("check", check))
	require.NoError(h.RegisterLivenessCheck("check", check))

	h.Start(t.Context(), checkFreq)
	defer h.Stop()

	awaitReadiness(t, h, true)
	awaitHealthy(t, h, true)
	awaitLiveness(t, h, true)

	{
		_, readiness := h.Readiness()
		require.True(readiness)

		_, health := h.Health()
		require.True(health)

		_, liveness := h.Liveness()
		require.True(liveness)
	}

	shouldCheckErr.Set(true)

	awaitHealthy(t, h, false)
	awaitLiveness(t, h, false)

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

	h.Start(t.Context(), time.Nanosecond)
	defer h.Stop()

	for i := 0; i < 100; i++ {
		lock.Lock()
		require.NoError(h.RegisterHealthCheck(fmt.Sprintf("check-%d", i), check))
		lock.Unlock()
	}

	awaitHealthy(t, h, true)
}

func TestTags(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(h.RegisterHealthCheck("check1", check))
	require.NoError(h.RegisterHealthCheck("check2", check, "tag1"))
	require.NoError(h.RegisterHealthCheck("check3", check, "tag2"))
	require.NoError(h.RegisterHealthCheck("check4", check, "tag1", "tag2"))
	require.NoError(h.RegisterHealthCheck("check5", check, ApplicationTag))

	// default checks
	{
		healthResult, health := h.Health()
		require.Len(healthResult, 5)
		require.Contains(healthResult, "check1")
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.False(health)

		healthResult, health = h.Health("tag1")
		require.Len(healthResult, 3)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.False(health)

		healthResult, health = h.Health("tag1", "tag2")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.False(health)

		healthResult, health = h.Health("nonExistentTag")
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check5")
		require.False(health)

		healthResult, health = h.Health("tag1", "tag2", "nonExistentTag")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.False(health)
	}

	h.Start(t.Context(), checkFreq)

	awaitHealthy(t, h, true)

	{
		healthResult, health := h.Health()
		require.Len(healthResult, 5)
		require.Contains(healthResult, "check1")
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.True(health)

		healthResult, health = h.Health("tag1")
		require.Len(healthResult, 3)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.True(health)

		healthResult, health = h.Health("tag1", "tag2")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.True(health)

		healthResult, health = h.Health("nonExistentTag")
		require.Len(healthResult, 1)
		require.Contains(healthResult, "check5")
		require.True(health)

		healthResult, health = h.Health("tag1", "tag2", "nonExistentTag")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.True(health)
	}

	// stop the health check
	h.Stop()

	{
		// now we'll add a new check which is unhealthy by default (notYetRunResult)
		require.NoError(h.RegisterHealthCheck("check6", check, "tag1"))

		awaitHealthy(t, h, false)

		healthResult, health := h.Health("tag1")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check2")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.Contains(healthResult, "check6")
		require.Equal(notYetRunResult, healthResult["check6"])
		require.False(health)

		healthResult, health = h.Health("tag2")
		require.Len(healthResult, 3)
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.True(health)

		// add application tag
		require.NoError(h.RegisterHealthCheck("check7", check, ApplicationTag))

		awaitHealthy(t, h, false)

		healthResult, health = h.Health("tag2")
		require.Len(healthResult, 4)
		require.Contains(healthResult, "check3")
		require.Contains(healthResult, "check4")
		require.Contains(healthResult, "check5")
		require.Contains(healthResult, "check7")
		require.Equal(notYetRunResult, healthResult["check7"])
		require.False(health)
	}
}
