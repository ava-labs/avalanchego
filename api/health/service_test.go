// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"net/http"
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
		err = s.Readiness(nil, &APIArgs{}, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Health(nil, &APIArgs{}, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Liveness(nil, &APIArgs{}, &reply)
		require.NoError(err)

		require.Len(reply.Checks, 1)
		require.Contains(reply.Checks, "check")
		require.Equal(notYetRunResult, reply.Checks["check"])
		require.False(reply.Healthy)
	}

	h.Start(context.Background(), checkFreq)
	defer h.Stop()

	awaitReadiness(t, h, true)
	awaitHealthy(t, h, true)
	awaitLiveness(t, h, true)

	{
		reply := APIReply{}
		err = s.Readiness(nil, &APIArgs{}, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Health(nil, &APIArgs{}, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}

	{
		reply := APIReply{}
		err = s.Liveness(nil, &APIArgs{}, &reply)
		require.NoError(err)

		result := reply.Checks["check"]
		require.Equal("", result.Details)
		require.Nil(result.Error)
		require.Zero(result.ContiguousFailures)
		require.True(reply.Healthy)
	}
}

func TestServiceTagResponse(t *testing.T) {
	check := CheckerFunc(func(context.Context) (interface{}, error) {
		return "", nil
	})

	subnetID1 := ids.GenerateTestID()
	subnetID2 := ids.GenerateTestID()

	// test cases
	type testMethods struct {
		name     string
		register func(Health, string, Checker, ...string) error
		check    func(*Service, *http.Request, *APIArgs, *APIReply) error
		await    func(*testing.T, Reporter, bool)
	}

	tests := []testMethods{
		{
			name: "Readiness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterReadinessCheck(s1, c, s2...)
			},
			check: func(s *Service, req *http.Request, a1 *APIArgs, a2 *APIReply) error {
				return s.Readiness(req, a1, a2)
			},
			await: awaitReadiness,
		},
		{
			name: "Health",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterHealthCheck(s1, c, s2...)
			},
			check: func(s *Service, r *http.Request, a1 *APIArgs, a2 *APIReply) error {
				return s.Health(r, a1, a2)
			},
			await: awaitHealthy,
		},
		{
			name: "Liveness",
			register: func(h Health, s1 string, c Checker, s2 ...string) error {
				return h.RegisterLivenessCheck(s1, c, s2...)
			},
			check: func(s *Service, r *http.Request, a1 *APIArgs, a2 *APIReply) error {
				return s.Liveness(r, a1, a2)
			},
			await: awaitLiveness,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			h, err := New(logging.NoLog{}, prometheus.NewRegistry())
			require.NoError(err)
			err = test.register(h, "check1", check)
			require.NoError(err)
			err = test.register(h, "check2", check, subnetID1.String())
			require.NoError(err)
			err = test.register(h, "check3", check, subnetID2.String())
			require.NoError(err)
			err = test.register(h, "check4", check, subnetID1.String(), subnetID2.String())
			require.NoError(err)

			s := &Service{
				log:    logging.NoLog{},
				health: h,
			}

			// default checks
			{
				reply := APIReply{}
				err = test.check(s, nil, &APIArgs{}, &reply)
				require.NoError(err)
				require.Len(reply.Checks, 4)
				require.Contains(reply.Checks, "check1")
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check3")
				require.Contains(reply.Checks, "check4")
				require.Equal(notYetRunResult, reply.Checks["check1"])
				require.False(reply.Healthy)

				err = test.check(s, nil, &APIArgs{Tags: []string{subnetID1.String()}}, &reply)
				require.NoError(err)
				require.Len(reply.Checks, 2)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.Equal(notYetRunResult, reply.Checks["check2"])
				require.False(reply.Healthy)
			}

			h.Start(context.Background(), checkFreq)

			test.await(t, h, true)

			{
				reply := APIReply{}
				err = test.check(s, nil, &APIArgs{Tags: []string{subnetID1.String()}}, &reply)
				require.NoError(err)
				require.Len(reply.Checks, 2)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.True(reply.Healthy)
			}

			// stop the health check
			h.Stop()

			{
				// now we'll add a new check which is unhealthy by default (notYetRunResult)
				err = test.register(h, "check5", check, subnetID1.String())
				require.NoError(err)

				reply := APIReply{}
				err = test.check(s, nil, &APIArgs{Tags: []string{subnetID1.String()}}, &reply)
				require.NoError(err)
				require.Len(reply.Checks, 3)
				require.Contains(reply.Checks, "check2")
				require.Contains(reply.Checks, "check4")
				require.Contains(reply.Checks, "check5")
				require.Equal(notYetRunResult, reply.Checks["check5"])
				require.False(reply.Healthy)
			}
		})
	}
}
