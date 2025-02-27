// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

type fakeBootstrappingTracker struct {
	res []ids.ID
}

func (f *fakeBootstrappingTracker) Bootstrapping() []ids.ID {
	return f.res
}

type fakeValidatorRetriever struct {
	result bool
}

func (m *fakeValidatorRetriever) GetValidator(ids.ID, ids.NodeID) (*validators.Validator, bool) {
	return nil, m.result
}

type fakeIngressConnectionCounter struct {
	res int
}

func (m *fakeIngressConnectionCounter) IngressConnCount() int {
	return m.res
}

type noIngressConnAlertTestInvocation struct {
	ingressConnCountResult int
	now                    func() time.Time
	expectedErr            error
	expectedResult         interface{}
}

func TestNoIngressConnAlertHealthCheck(t *testing.T) {
	start := time.Now()

	for _, testCase := range []struct {
		name                string
		getValidatorResult  bool
		bootstrappingResult []ids.ID
		inputOutputs        []noIngressConnAlertTestInvocation
	}{
		{
			name:                "still bootstrapping",
			bootstrappingResult: []ids.ID{{}},
			inputOutputs:        []noIngressConnAlertTestInvocation{{now: time.Now}},
		},
		{
			name: "some are connected to us",
			inputOutputs: []noIngressConnAlertTestInvocation{{
				ingressConnCountResult: 42,
				now:                    time.Now,
			}},
		},
		{
			name: "first check",
			inputOutputs: []noIngressConnAlertTestInvocation{{
				now: time.Now,
			}},
		},
		{
			name:               "validator but not enough time has passed between checks",
			getValidatorResult: true,
			inputOutputs: []noIngressConnAlertTestInvocation{{
				now: func() time.Time { return start },
			}, {
				now: func() time.Time { return start.Add(time.Minute) },
			}},
		},
		{
			name: "enough time has passed between checks but not a validator",
			inputOutputs: []noIngressConnAlertTestInvocation{
				{
					now: func() time.Time { return start },
				}, {
					now:            func() time.Time { return start.Add(time.Hour) },
					expectedResult: map[string]interface{}{"ingressConnectionCount": 0, "primary network validator": false},
				},
			},
		},
		{
			name:               "enough time has passed between checks and also a validator",
			getValidatorResult: true,
			inputOutputs: []noIngressConnAlertTestInvocation{{
				now: func() time.Time { return start },
			}, {
				now:         func() time.Time { return start.Add(time.Hour) },
				expectedErr: ErrNoIngressConnections,
				expectedResult: map[string]interface{}{
					"ingressConnectionCount": 0, "primary network validator": true,
				},
			}},
		},
		{
			name: "recovery",
			inputOutputs: []noIngressConnAlertTestInvocation{{
				now: func() time.Time { return start },
			}, {
				now:         func() time.Time { return start.Add(time.Hour) },
				expectedErr: ErrNoIngressConnections,
				expectedResult: map[string]interface{}{
					"ingressConnectionCount": 0, "primary network validator": true,
				},
			}, {
				ingressConnCountResult: 42,
				now:                    func() time.Time { return start.Add(time.Hour).Add(time.Minute) },
				expectedResult:         map[string]interface{}{"ingressConnectionCount": 42, "primary network validator": true},
			}},
			getValidatorResult: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			nica := &noIngressConnAlert{
				selfID:           ids.EmptyNodeID,
				minCheckInterval: 20 * time.Minute,
				bootstrapping:    &fakeBootstrappingTracker{res: testCase.bootstrappingResult},
				validators:       &fakeValidatorRetriever{result: testCase.getValidatorResult},
			}

			for _, io := range testCase.inputOutputs {
				nica.now = io.now
				nica.ingressConnections = &fakeIngressConnectionCounter{res: io.ingressConnCountResult}
				result, err := nica.HealthCheck(context.Background())
				require.Equal(t, io.expectedErr, err)
				require.Equal(t, io.expectedResult, result)
			}
		})
	}
}
