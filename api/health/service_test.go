// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/utils/logging"
)

func TestServiceResponses(t *testing.T) {
	assert := assert.New(t)

	check := CheckerFunc(func() (interface{}, error) {
		return "", nil
	})

	h, err := New(prometheus.NewRegistry())
	assert.NoError(err)

	s := &Service{
		log:    logging.NoLog{},
		health: h,
	}

	err = h.RegisterReadinessCheck("check", check)
	assert.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	assert.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	assert.NoError(err)

	{
		reply := APIHealthReply{}
		err = s.Readiness(nil, nil, &reply)
		assert.NoError(err)

		assert.Len(reply.Checks, 1)
		assert.Contains(reply.Checks, "check")
		assert.Equal(notYetRunResult, reply.Checks["check"])
		assert.False(reply.Healthy)
	}

	{
		reply := APIHealthReply{}
		err = s.Health(nil, nil, &reply)
		assert.NoError(err)

		assert.Len(reply.Checks, 1)
		assert.Contains(reply.Checks, "check")
		assert.Equal(notYetRunResult, reply.Checks["check"])
		assert.False(reply.Healthy)
	}

	{
		reply := APIHealthReply{}
		err = s.Liveness(nil, nil, &reply)
		assert.NoError(err)

		assert.Len(reply.Checks, 1)
		assert.Contains(reply.Checks, "check")
		assert.Equal(notYetRunResult, reply.Checks["check"])
		assert.False(reply.Healthy)
	}

	h.Start(checkFreq)
	defer h.Stop()

	awaitReadiness(h)
	awaitHealthy(h, true)
	awaitLiveness(h, true)

	{
		reply := APIHealthReply{}
		err = s.Readiness(nil, nil, &reply)
		assert.NoError(err)

		result := reply.Checks["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(reply.Healthy)
	}

	{
		reply := APIHealthReply{}
		err = s.Health(nil, nil, &reply)
		assert.NoError(err)

		result := reply.Checks["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(reply.Healthy)
	}

	{
		reply := APIHealthReply{}
		err = s.Liveness(nil, nil, &reply)
		assert.NoError(err)

		result := reply.Checks["check"]
		assert.Equal("", result.Details)
		assert.Nil(result.Error)
		assert.Zero(result.ContiguousFailures)
		assert.True(reply.Healthy)
	}
}
