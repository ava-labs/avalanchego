// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

func TestServiceResponses(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	s := &Service{
		log:    logging.NoLog{},
		health: h,
	}

	err = h.RegisterReadinessCheck("check", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check", check)
	require.NoError(err)
	err = h.RegisterLivenessCheck("check", check)
	require.NoError(err)

	{
		reply := APIReply{}
		err = s.Readiness(nil, nil, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Health(nil, nil, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Liveness(nil, nil, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitReadiness(h)
	awaitHealthy(h, true)
	awaitLiveness(h, true)

	{
		reply := APIReply{}
		err = s.Readiness(nil, nil, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Health(nil, nil, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Liveness(nil, nil, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}
}

func TestServiceTagResponse(t *testing.T) {
	require := require.New(t)

	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	subnetID1 := ids.GenerateTestID()
	subnetID2 := ids.GenerateTestID()

	h, err := New(logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)
	err = h.RegisterHealthCheck("check1", check)
	require.NoError(err)
	err = h.RegisterHealthCheck("check2", check, subnetID1.String())
	require.NoError(err)
	err = h.RegisterHealthCheck("check3", check, subnetID2.String())
	require.NoError(err)
	err = h.RegisterHealthCheck("check4", check, subnetID1.String(), subnetID2.String())
	require.NoError(err)

	s := &Service{
		log:    logging.NoLog{},
		health: h,
	}

	// default checks
	{
		reply := APIReply{}
		err = s.Health(nil, nil, &reply)
		require.NoError(err)
		require.Len(reply.Checks, 4)
		require.Contains(reply.Checks, "check1")
		require.Contains(reply.Checks, "check2")
		require.Contains(reply.Checks, "check3")
		require.Contains(reply.Checks, "check4")
		require.Equal(notYetRunResult, reply.Checks["check1"])
		require.False(reply.Healthy)

		err = s.Health(nil, &HealthArgs{SubnetIDs: []ids.ID{subnetID1}}, &reply)
		require.NoError(err)
		require.Len(reply.Checks, 2)
		require.Contains(reply.Checks, "check2")
		require.Contains(reply.Checks, "check4")
		require.Equal(notYetRunResult, reply.Checks["check2"])
		require.False(reply.Healthy)
	}

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitHealthy(h, true)

	{
		reply := APIReply{}
		err = s.Health(nil, &HealthArgs{SubnetIDs: []ids.ID{subnetID1}}, &reply)
		require.NoError(err)
		require.Len(reply.Checks, 2)
		require.Contains(reply.Checks, "check2")
		require.Contains(reply.Checks, "check4")
		require.True(reply.Healthy)
	}

	// now we'll add a new failing check
	{
		err = h.RegisterHealthCheck("check5", check, subnetID1.String())
		require.NoError(err)

		reply := APIReply{}
		err = s.Health(nil, &HealthArgs{SubnetIDs: []ids.ID{subnetID1}}, &reply)
		require.NoError(err)
		require.Len(reply.Checks, 3)
		require.Contains(reply.Checks, "check2")
		require.Contains(reply.Checks, "check4")
		require.Contains(reply.Checks, "check5")
		require.Equal(notYetRunResult, reply.Checks["check5"])
		require.False(reply.Healthy)
	}
}
