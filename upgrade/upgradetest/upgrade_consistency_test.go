// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgradetest

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// TestUpgradeConsistency ensures that when a new upgrade is added, all
// necessary locations are updated.
func TestUpgradeConsistency(t *testing.T) {
	configType := reflect.TypeOf(upgrade.Config{})
	var timeFields []string

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		if field.Type == reflect.TypeOf(time.Time{}) {
			timeFields = append(timeFields, field.Name)
		}
	}

	t.Run("all time fields are in Validate method", func(*testing.T) {
		// Test that Validate() doesn't return an error when all times are equal
		testConfig := upgrade.Config{}
		configValue := reflect.ValueOf(&testConfig).Elem()

		testTime := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		for _, fieldName := range timeFields {
			field := configValue.FieldByName(fieldName)
			require.True(t, field.IsValid(), "Field %s should exist", fieldName)
			field.Set(reflect.ValueOf(testTime))
		}

		// If all times are equal, Validate should pass
		require.NoError(t, testConfig.Validate(), "Validate should pass when all upgrade times are equal")

		// Now test that Validate catches out-of-order times
		// Set the last time field to before the first
		if len(timeFields) >= 2 {
			lastField := configValue.FieldByName(timeFields[len(timeFields)-1])
			lastField.Set(reflect.ValueOf(testTime.Add(-time.Hour)))
			err := testConfig.Validate()
			require.ErrorIs(t, err, upgrade.ErrInvalidUpgradeTimes, "Validate should fail when upgrade times are out of order")
		}
	})

	t.Run("all time fields are set in network configs", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			config uint32
		}{
			{"Mainnet", constants.MainnetID},
			{"Fuji", constants.FujiID},
			{"Default", constants.LocalID},
		} {
			t.Run(tc.name, func(*testing.T) {
				for fork := ApricotPhase1; fork <= Latest; fork++ {
					timeValue := GetActivationTime(fork, tc.config)
					require.False(t, timeValue.IsZero(), "%sTime on %s should be set to a non-zero time", fork, tc.name)
				}
			})
		}
	})

	t.Run("all Fork constants are properly stringifiable and vice versa", func(*testing.T) {
		// Check that each fork is bi-directionally stringifiable
		for fork := NoUpgrades; fork <= Latest+1; fork++ {
			forkStr := fork.String()

			// Forks beyond Latest should return "Unknown"
			if fork > Latest {
				require.Equal(t, "Unknown", forkStr, "Fork %d (beyond Latest) should return 'Unknown'", fork)
				continue
			}

			require.NotEqual(t, "Unknown", forkStr, "Fork %d should be stringifiable", fork)
			parsedFork := FromString(forkStr)
			require.Equal(t, fork, parsedFork, "FromString(%q) should return %d, got %d", forkStr, fork, parsedFork)
		}

		// Check that an invalid string fails
		invalidName := "thehimaruupgrade"
		fork := FromString(invalidName)
		require.Equal(t, Fork(-1), fork, "FromString(%q) should return -1 for invalid name", invalidName)
	})

	t.Run("upgradetest.SetTimesTo handles all forks", func(t *testing.T) {
		// This test ensures SetTimesTo properly sets the specified fork and all prior forks
		testTime := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)

		for targetFork := ApricotPhase1; targetFork <= Latest; targetFork++ {
			forkName := targetFork.String()

			t.Run(forkName, func(*testing.T) {
				config := upgrade.Config{}
				SetTimesTo(&config, targetFork, testTime)
				configValue := reflect.ValueOf(config)

				// Verify that the target fork and all prior forks are set to testTime
				for checkFork := ApricotPhase1; checkFork <= targetFork; checkFork++ {
					checkForkName := checkFork.String()
					fieldName := checkForkName + "Time"
					field := configValue.FieldByName(fieldName)

					if field.IsValid() {
						require.Equal(t, reflect.TypeOf(time.Time{}), field.Type(),
							"Field %s should be time.Time type", fieldName)
						timeVal := field.Interface().(time.Time)
						require.True(t, timeVal.Equal(testTime),
							"SetTimesTo(%s, testTime) should set %s to testTime, but got %v",
							forkName, fieldName, timeVal)
					}
				}

				// Verify that forks after the target fork are NOT set to testTime
				for checkFork := targetFork + 1; checkFork <= Latest; checkFork++ {
					checkForkName := checkFork.String()
					fieldName := checkForkName + "Time"
					field := configValue.FieldByName(fieldName)

					if field.IsValid() {
						timeVal := field.Interface().(time.Time)
						require.False(t, timeVal.Equal(testTime),
							"SetTimesTo(%s, testTime) should NOT set %s to testTime, but it did",
							forkName, fieldName)
					}
				}
			})
		}
	})
}

// TestUpgradeFieldNaming ensures consistent naming conventions across the codebase.
func TestUpgradeFieldNaming(t *testing.T) {
	configType := reflect.TypeOf(upgrade.Config{})

	t.Run("json tags use camelCase", func(*testing.T) {
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			jsonTag := field.Tag.Get("json")
			require.NotEmpty(t, jsonTag, "Field %s must have a json tag", field.Name)
			require.Equal(t, //nolint:testifylint // these are not json strings
				strings.ToLower(field.Name[:1])+field.Name[1:],
				jsonTag,
				"json tag %s must be the lower-camel version of field %s",
				jsonTag, field.Name,
			)
		}
	})
}
