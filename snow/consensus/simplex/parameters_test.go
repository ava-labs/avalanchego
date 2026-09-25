// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParametersVerify(t *testing.T) {
	validValidators := []ValidatorInfo{{}}

	tests := []struct {
		name         string
		params       Parameters
		expectedErrs []error
	}{
		{
			name: "valid",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedErrs: nil,
		},
		{
			name: "zero MaxNetworkDelay",
			params: Parameters{
				MaxNetworkDelay:    0,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  validValidators,
			},
			expectedErrs: []error{errMaxNetworkDelayNotPositive},
		},
		{
			name: "zero MaxRebroadcastWait",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: 0,
				InitialValidators:  validValidators,
			},
			expectedErrs: []error{errMaxRebroadcastWaitNotPositive},
		},
		{
			name: "empty InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  []ValidatorInfo{},
			},
			expectedErrs: []error{errInitialValidatorsEmpty},
		},
		{
			name: "nil InitialValidators",
			params: Parameters{
				MaxNetworkDelay:    time.Second,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  nil,
			},
			expectedErrs: []error{errInitialValidatorsEmpty},
		},
		{
			name: "multiple invalid",
			params: Parameters{
				MaxNetworkDelay:    0,
				MaxRebroadcastWait: 0,
				InitialValidators:  validValidators,
			},
			expectedErrs: []error{
				errMaxNetworkDelayNotPositive,
				errMaxRebroadcastWaitNotPositive,
			},
		},
		{
			name:   "zero value",
			params: Parameters{},
			expectedErrs: []error{
				errMaxNetworkDelayNotPositive,
				errMaxRebroadcastWaitNotPositive,
				errInitialValidatorsEmpty,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			if len(test.expectedErrs) == 0 {
				require.NoError(t, err)
				return
			}

			require.ErrorIs(t, err, ErrInvalidParameters)
			for _, expectedErr := range test.expectedErrs {
				require.ErrorIs(t, err, expectedErr)
			}
			// Verify joins one error per violated condition, so every expected
			// condition being present and the counts matching means nothing
			// else was reported.
			require.Len(t, joinedErrs(t, err), len(test.expectedErrs))
		})
	}
}

// joinedErrs returns the individual errors combined by [errors.Join].
func joinedErrs(t *testing.T, err error) []error {
	joined, ok := err.(interface{ Unwrap() []error })
	require.True(t, ok, "expected an error produced by errors.Join")
	return joined.Unwrap()
}

func TestParametersVerifyErrorMessage(t *testing.T) {
	tests := []struct {
		name            string
		params          Parameters
		expectedMessage string
	}{
		{
			name: "single violation",
			params: Parameters{
				MaxNetworkDelay:    0,
				MaxRebroadcastWait: time.Second,
				InitialValidators:  []ValidatorInfo{{}},
			},
			expectedMessage: "simplex parameters must be valid: maxNetworkDelay must be positive",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			require.ErrorIs(t, err, ErrInvalidParameters)
			require.Equal(t, test.expectedMessage, err.Error())
		})
	}
}
