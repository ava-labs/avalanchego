// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils"
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
	assert := assert.New(t)

	check := CheckerFunc(func() (interface{}, error) {
		return "", nil
	})

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	assert.NoError(err)
	err = h.RegisterReadinessCheck("check", check)
	assert.ErrorIs(err, errDuplicateCheck)

	err = h.RegisterHealthCheck("check", check)
	assert.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	assert.ErrorIs(err, errDuplicateCheck)

	err = h.RegisterLivenessCheck("check", check)
	assert.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	assert.ErrorIs(err, errDuplicateCheck)
}

func TestDefaultFailing(t *testing.T) {
	assert := assert.New(t)

	check := CheckerFunc(func() (interface{}, error) {
		return "", nil
	})

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	{
		err = h.RegisterReadinessCheck("check", check)
		assert.NoError(err)

		readinessResult, readiness := h.Readiness()
		assert.Len(readinessResult, 1)
		assert.Contains(readinessResult, "check")
		assert.Equal(notYetRunResult, readinessResult["check"])
		assert.False(readiness)
	}

	{
		err = h.RegisterHealthCheck("check", check)
		assert.NoError(err)

		healthResult, health := h.Health()
		assert.Len(healthResult, 1)
		assert.Contains(healthResult, "check")
		assert.Equal(notYetRunResult, healthResult["check"])
		assert.False(health)
	}

	{
		err = h.RegisterLivenessCheck("check", check)
		assert.NoError(err)

		livenessResult, liveness := h.Liveness()
		assert.Len(livenessResult, 1)
		assert.Contains(livenessResult, "check")
		assert.Equal(notYetRunResult, livenessResult["check"])
		assert.False(liveness)
	}
}

func TestPassingChecks(t *testing.T) {
	assert := assert.New(t)

	check := CheckerFunc(func() (interface{}, error) {
		return "", nil
	})

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	assert.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	assert.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	assert.NoError(err)

	h.Start(checkFreq)
	defer h.Stop()

	{
		awaitReadiness(h)

		readinessResult, readiness := h.Readiness()
		assert.Len(readinessResult, 1)
		assert.Contains(readinessResult, "check")

		result := readinessResult["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(readiness)
	}

	{
		awaitHealthy(h, true)

		healthResult, health := h.Health()
		assert.Len(healthResult, 1)
		assert.Contains(healthResult, "check")

		result := healthResult["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(health)
	}

	{
		awaitLiveness(h, true)

		livenessResult, liveness := h.Liveness()
		assert.Len(livenessResult, 1)
		assert.Contains(livenessResult, "check")

		result := livenessResult["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(liveness)
	}
}

func TestPassingThenFailingChecks(t *testing.T) {
	assert := assert.New(t)

	var (
		shouldCheckErr utils.AtomicBool
		checkErr       = errors.New("unhealthy")
	)
	check := CheckerFunc(func() (interface{}, error) {
		if shouldCheckErr.GetValue() {
			return checkErr.Error(), checkErr
		}
		return "", nil
	})

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	err = h.RegisterReadinessCheck("check", check)
	assert.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	assert.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	assert.NoError(err)

	h.Start(checkFreq)
	defer h.Stop()

	awaitReadiness(h)
	awaitHealthy(h, true)
	awaitLiveness(h, true)

	{
		_, readiness := h.Readiness()
		assert.True(readiness)

		_, health := h.Health()
		assert.True(health)

		_, liveness := h.Liveness()
		assert.True(liveness)
	}

	shouldCheckErr.SetValue(true)

	awaitHealthy(h, false)
	awaitLiveness(h, false)

	{
		// Notice that Readiness is a monotonic check - so it still reports
		// ready.
		_, readiness := h.Readiness()
		assert.True(readiness)

		_, health := h.Health()
		assert.False(health)

		_, liveness := h.Liveness()
		assert.False(liveness)
	}
}

func TestDeadlockRegression(t *testing.T) {
	assert := assert.New(t)

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	var lock sync.Mutex
	check := CheckerFunc(func() (interface{}, error) {
		lock.Lock()
		time.Sleep(time.Nanosecond)
		lock.Unlock()
		return "", nil
	})

	h.Start(time.Nanosecond)
	defer h.Stop()

	for i := 0; i < 1000; i++ {
		lock.Lock()
		err = h.RegisterHealthCheck(fmt.Sprintf("check-%d", i), check)
		lock.Unlock()
		assert.NoError(err)
	}

	awaitHealthy(h, true)
}
