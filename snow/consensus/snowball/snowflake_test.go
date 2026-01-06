// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "testing"

const alphaPreference = 3

var terminationConditions = []terminationCondition{
	{
		alphaConfidence: 3,
		beta:            4,
	},
	{
		alphaConfidence: 4,
		beta:            3,
	},
	{
		alphaConfidence: 5,
		beta:            2,
	},
}

type snowflakeTestConstructor[T comparable] func(t *testing.T, alphaPreference int, terminationConditions []terminationCondition) snowflakeTest[T]

type snowflakeTest[T comparable] interface {
	RecordPoll(count int, optionalMode T)
	RecordUnsuccessfulPoll()
	AssertEqual(expectedConfidences []int, expectedFinalized bool, expectedPreference T)
}

func executeErrorDrivenTerminatesInBetaPolls[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	for i, terminationCondition := range terminationConditions {
		sfTest := newSnowflakeTest(t, alphaPreference, terminationConditions)

		for poll := 0; poll < terminationCondition.beta; poll++ {
			sfTest.RecordPoll(terminationCondition.alphaConfidence, choice)

			expectedConfidences := make([]int, len(terminationConditions))
			for j := 0; j < i+1; j++ {
				expectedConfidences[j] = poll + 1
			}
			sfTest.AssertEqual(expectedConfidences, poll+1 >= terminationCondition.beta, choice)
		}
	}
}

func executeErrorDrivenReset[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	for i, terminationCondition := range terminationConditions {
		sfTest := newSnowflakeTest(t, alphaPreference, terminationConditions)

		// Accumulate confidence up to 1 less than beta, reset, and confirm
		// expected behavior from fresh state.
		for poll := 0; poll < terminationCondition.beta-1; poll++ {
			sfTest.RecordPoll(terminationCondition.alphaConfidence, choice)
		}
		sfTest.RecordUnsuccessfulPoll()
		zeroConfidence := make([]int, len(terminationConditions))
		sfTest.AssertEqual(zeroConfidence, false, choice)

		for poll := 0; poll < terminationCondition.beta; poll++ {
			sfTest.RecordPoll(terminationCondition.alphaConfidence, choice)

			expectedConfidences := make([]int, len(terminationConditions))
			for j := 0; j < i+1; j++ {
				expectedConfidences[j] = poll + 1
			}
			sfTest.AssertEqual(expectedConfidences, poll+1 >= terminationCondition.beta, choice)
		}
	}
}

func executeErrorDrivenResetHighestAlphaConfidence[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice T) {
	sfTest := newSnowflakeTest(t, alphaPreference, terminationConditions)

	sfTest.RecordPoll(5, choice)
	sfTest.AssertEqual([]int{1, 1, 1}, false, choice)
	sfTest.RecordPoll(4, choice)
	sfTest.AssertEqual([]int{2, 2, 0}, false, choice)
	sfTest.RecordPoll(3, choice)
	sfTest.AssertEqual([]int{3, 0, 0}, false, choice)
	sfTest.RecordPoll(5, choice)
	sfTest.AssertEqual([]int{4, 0, 0}, true, choice)
}

type snowflakeTestSingleChoice[T comparable] struct {
	name string
	f    func(*testing.T, snowflakeTestConstructor[T], T)
}

func getErrorDrivenSnowflakeSingleChoiceSuite[T comparable]() []snowflakeTestSingleChoice[T] {
	return []snowflakeTestSingleChoice[T]{
		{
			name: "TerminateInBetaPolls",
			f:    executeErrorDrivenTerminatesInBetaPolls[T],
		},
		{
			name: "Reset",
			f:    executeErrorDrivenReset[T],
		},
		{
			name: "ResetHighestAlphaConfidence",
			f:    executeErrorDrivenResetHighestAlphaConfidence[T],
		},
	}
}

func executeErrorDrivenSwitchChoices[T comparable](t *testing.T, newSnowflakeTest snowflakeTestConstructor[T], choice0, choice1 T) {
	sfTest := newSnowflakeTest(t, alphaPreference, terminationConditions)

	sfTest.RecordPoll(3, choice0)
	sfTest.AssertEqual([]int{1, 0, 0}, false, choice0)

	sfTest.RecordPoll(2, choice1)
	sfTest.AssertEqual([]int{0, 0, 0}, false, choice0)

	sfTest.RecordPoll(3, choice0)
	sfTest.AssertEqual([]int{1, 0, 0}, false, choice0)

	sfTest.RecordPoll(0, choice0)
	sfTest.AssertEqual([]int{0, 0, 0}, false, choice0)

	sfTest.RecordPoll(3, choice1)
	sfTest.AssertEqual([]int{1, 0, 0}, false, choice1)

	sfTest.RecordPoll(5, choice1)
	sfTest.AssertEqual([]int{2, 1, 1}, false, choice1)
	sfTest.RecordPoll(5, choice1)
	sfTest.AssertEqual([]int{3, 2, 2}, true, choice1)
}

type snowflakeTestMultiChoice[T comparable] struct {
	name string
	f    func(*testing.T, snowflakeTestConstructor[T], T, T)
}

func getErrorDrivenSnowflakeMultiChoiceSuite[T comparable]() []snowflakeTestMultiChoice[T] {
	return []snowflakeTestMultiChoice[T]{
		{
			name: "SwitchChoices",
			f:    executeErrorDrivenSwitchChoices[T],
		},
	}
}
