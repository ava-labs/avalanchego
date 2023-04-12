// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
		awaitReadiness(t, h, true)

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
		awaitHealthy(t, h, true)

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
		awaitLiveness(t, h, true)

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

	var shouldCheckErr utils.Atomic[bool]
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		if shouldCheckErr.Get() {
			return errUnhealthy.Error(), errUnhealthy
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

	h.Start(context.Background(), time.Nanosecond)
	defer h.Stop()

	for i := 0; i < 1000; i++ {
		lock.Lock()
		err = h.RegisterHealthCheck(fmt.Sprintf("check-%d", i), check)
		lock.Unlock()
		require.NoError(err)
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
	err = h.RegisterHealthCheck("check1", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check2", check, "tag1")
	require.NoError(err)
	err = h.RegisterHealthCheck("check3", check, "tag2")
	require.NoError(err)
	err = h.RegisterHealthCheck("check4", check, "tag1", "tag2")
	require.NoError(err)
	err = h.RegisterHealthCheck("check5", check, GlobalTag)
	require.NoError(err)

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

	h.Start(context.Background(), checkFreq)

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
		err = h.RegisterHealthCheck("check6", check, "tag1")
		require.NoError(err)

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

		// add global tag
		err = h.RegisterHealthCheck("check7", check, GlobalTag)
		require.NoError(err)

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
