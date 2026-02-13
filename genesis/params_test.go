// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUptimeRequirementConfigVerify(t *testing.T) {
	tests := map[string]struct {
		config      UptimeRequirementConfig
		expectedErr error
	}{
		"valid - default only": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  0.8,
				RequiredUptimePercentageSchedule: nil,
			},
			expectedErr: nil,
		},
		"valid - default with single update": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
				},
			},
			expectedErr: nil,
		},
		"valid - default with multiple updates": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
						Requirement: 0.95,
					},
				},
			},
			expectedErr: nil,
		},
		"valid - zero default": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  0,
				RequiredUptimePercentageSchedule: nil,
			},
			expectedErr: nil,
		},
		"valid - one default": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  1,
				RequiredUptimePercentageSchedule: nil,
			},
			expectedErr: nil,
		},
		"invalid - negative default": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  -0.1,
				RequiredUptimePercentageSchedule: nil,
			},
			expectedErr: errInvalidUptimeRequirement,
		},
		"invalid - default above 1": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  1.1,
				RequiredUptimePercentageSchedule: nil,
			},
			expectedErr: errInvalidUptimeRequirement,
		},
		"invalid - negative requirement in schedule": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: -0.1,
					},
				},
			},
			expectedErr: errInvalidUptimeRequirement,
		},
		"invalid - requirement above 1 in schedule": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 1.5,
					},
				},
			},
			expectedErr: errInvalidUptimeRequirement,
		},
		"invalid - non-increasing times (equal)": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.95,
					},
				},
			},
			expectedErr: errUptimeRequirementTimeNotIncreasing,
		},
		"invalid - non-increasing times (decreasing)": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.95,
					},
				},
			},
			expectedErr: errUptimeRequirementTimeNotIncreasing,
		},
		"invalid - second requirement negative in multiple updates": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
						Requirement: -0.5,
					},
				},
			},
			expectedErr: errInvalidUptimeRequirement,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.config.Verify()
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func TestUptimeRequirementConfigRequiredUptime(t *testing.T) {
	tests := map[string]struct {
		config              UptimeRequirementConfig
		stakerStartTime     time.Time
		expectedRequirement float64
	}{
		"default - no schedule": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage:  0.8,
				RequiredUptimePercentageSchedule: nil,
			},
			stakerStartTime:     time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.8,
		},
		"default - staker started before first update": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
				},
			},
			stakerStartTime:     time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.8,
		},
		"updated - staker started on update time": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
				},
			},
			stakerStartTime:     time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.9,
		},
		"updated - staker started after update time": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
				},
			},
			stakerStartTime:     time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.9,
		},
		"first update - staker started before second update": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
						Requirement: 0.95,
					},
				},
			},
			stakerStartTime:     time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.9,
		},
		"second update - staker started after second update": {
			config: UptimeRequirementConfig{
				DefaultRequiredUptimePercentage: 0.8,
				RequiredUptimePercentageSchedule: []UptimeRequirementUpdate{
					{
						Time:        time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
						Requirement: 0.9,
					},
					{
						Time:        time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
						Requirement: 0.95,
					},
				},
			},
			stakerStartTime:     time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC),
			expectedRequirement: 0.95,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			actual := test.config.RequiredUptime(test.stakerStartTime)
			require.Equal(test.expectedRequirement, actual)
		})
	}
}
