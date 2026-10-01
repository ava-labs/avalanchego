// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParametersVerify(t *testing.T) {
	tests := []struct {
		name         string
		params       Parameters
		expectedErrs []error
	}{
		{
			name: "valid",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: nil,
		},
		{
			name: "invalid K",
			params: Parameters{
				K:                     0,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errAlphaConfidenceAboveK},
		},
		{
			name: "invalid AlphaPreference 1",
			params: Parameters{
				K:                     2,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errAlphaPreferenceNotAboveHalfK},
		},
		{
			name: "invalid AlphaPreference 0",
			params: Parameters{
				K:                     1,
				AlphaPreference:       0,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errAlphaPreferenceNotAboveHalfK},
		},
		{
			name: "invalid AlphaConfidence",
			params: Parameters{
				K:                     3,
				AlphaPreference:       3,
				AlphaConfidence:       2,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errAlphaConfidenceBelowAlphaPreference},
		},
		{
			name: "invalid beta",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  0,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errConcurrentRepollsAboveBeta},
		},
		{
			name: "first half fun alphaConfidence",
			params: Parameters{
				K:                     30,
				AlphaPreference:       28,
				AlphaConfidence:       30,
				Beta:                  2,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: nil,
		},
		{
			name: "second half fun alphaConfidence",
			params: Parameters{
				K:                     3,
				AlphaPreference:       2,
				AlphaConfidence:       3,
				Beta:                  2,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: nil,
		},
		{
			name: "fun invalid alphaConfidence",
			params: Parameters{
				K:                     1,
				AlphaPreference:       28,
				AlphaConfidence:       3,
				Beta:                  2,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errAlphaConfidenceBelowAlphaPreference, errAlphaConfidenceAboveK},
		},
		{
			name: "too few ConcurrentRepolls",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     0,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errConcurrentRepollsNotPositive},
		},
		{
			name: "too many ConcurrentRepolls",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     2,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errConcurrentRepollsAboveBeta},
		},
		{
			name: "invalid OptimalProcessing",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     0,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errOptimalProcessingNotPositive},
		},
		{
			name: "invalid MaxOutstandingItems",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   0,
				MaxItemProcessingTime: 1,
			},
			expectedErrs: []error{errMaxOutstandingItemsNotPositive},
		},
		{
			name: "invalid MaxItemProcessingTime",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 0,
			},
			expectedErrs: []error{errMaxItemProcessingTimeNotPositive},
		},
		{
			name: "multiple invalid",
			params: Parameters{
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     0,
				OptimalProcessing:     0,
				MaxOutstandingItems:   0,
				MaxItemProcessingTime: 0,
			},
			expectedErrs: []error{
				errConcurrentRepollsNotPositive,
				errOptimalProcessingNotPositive,
				errMaxOutstandingItemsNotPositive,
				errMaxItemProcessingTimeNotPositive,
			},
		},
		{
			name:   "zero value",
			params: Parameters{},
			expectedErrs: []error{
				errAlphaPreferenceNotAboveHalfK,
				errConcurrentRepollsNotPositive,
				errOptimalProcessingNotPositive,
				errMaxOutstandingItemsNotPositive,
				errMaxItemProcessingTimeNotPositive,
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

			require.ErrorIs(t, err, ErrParametersInvalid)
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
				K:                     1,
				AlphaPreference:       1,
				AlphaConfidence:       1,
				Beta:                  1,
				ConcurrentRepolls:     0,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedMessage: "parameters invalid: concurrentRepolls = 0: fails the condition that: 0 < concurrentRepolls",
		},
		{
			name: "fun invalid alphaConfidence",
			params: Parameters{
				K:                     30,
				AlphaPreference:       28,
				AlphaConfidence:       3,
				Beta:                  2,
				ConcurrentRepolls:     1,
				OptimalProcessing:     1,
				MaxOutstandingItems:   1,
				MaxItemProcessingTime: 1,
			},
			expectedMessage: "parameters invalid: alphaConfidence = 3, alphaPreference = 28: fails the condition that: alphaPreference <= alphaConfidence\n" +
				errMsg,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.params.Verify()
			require.ErrorIs(t, err, ErrParametersInvalid)
			require.Equal(t, test.expectedMessage, err.Error())
		})
	}
}

func TestParametersMinPercentConnectedHealthy(t *testing.T) {
	tests := []struct {
		name                        string
		params                      Parameters
		expectedMinPercentConnected float64
	}{
		{
			name:                        "default",
			params:                      DefaultParameters,
			expectedMinPercentConnected: 0.8,
		},
		{
			name: "custom",
			params: Parameters{
				K:               5,
				AlphaConfidence: 4,
			},
			expectedMinPercentConnected: 0.84,
		},
		{
			name: "custom",
			params: Parameters{
				K:               1001,
				AlphaConfidence: 501,
			},
			expectedMinPercentConnected: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minStake := tt.params.MinPercentConnectedHealthy()
			require.InEpsilon(t, tt.expectedMinPercentConnected, minStake, .001)
		})
	}
}
