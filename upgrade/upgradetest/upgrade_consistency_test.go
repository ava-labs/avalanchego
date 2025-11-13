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
)

// TestUpgradeConsistency ensures that when a new upgrade is added, all
// necessary locations are updated. This test helps prevent mistakes like
// forgetting to add a field to the Validate() method or missing a case in
// SetTimesTo().
func TestUpgradeConsistency(t *testing.T) {
	require := require.New(t)

	// Get all time.Time fields from upgrade.Config struct
	configType := reflect.TypeOf(upgrade.Config{})
	var (
		timeFields     []string
		durationFields []string
	)

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		if field.Type == reflect.TypeOf(time.Time{}) {
			timeFields = append(timeFields, field.Name)
		}
		if field.Type == reflect.TypeOf(time.Duration(0)) {
			durationFields = append(durationFields, field.Name)
		}
	}

	require.NotEmpty(timeFields, "upgrade.Config should have time.Time fields")

	t.Run("all time fields have activation methods", func(*testing.T) {
		configValueType := reflect.TypeOf(&upgrade.Config{})

		for _, fieldName := range timeFields {
			// Expected method name: IsXActivated
			// e.g., GraniteTime -> IsGraniteActivated
			upgradeName := strings.TrimSuffix(fieldName, "Time")
			expectedMethodName := "Is" + upgradeName + "Activated"

			method, found := configValueType.MethodByName(expectedMethodName)
			require.True(found, "upgrade.Config should have method %s for field %s", expectedMethodName, fieldName)

			// Verify method signature: func(*upgrade.Config, time.Time) bool
			require.Equal(2, method.Type.NumIn(), "Method %s should have 2 inputs (receiver, time.Time)", expectedMethodName)
			require.Equal(1, method.Type.NumOut(), "Method %s should have 1 output (bool)", expectedMethodName)
			require.Equal(reflect.TypeOf(time.Time{}), method.Type.In(1), "Method %s should take time.Time as parameter", expectedMethodName)
			require.Equal(reflect.TypeOf(true), method.Type.Out(0), "Method %s should return bool", expectedMethodName)
		}
	})

	t.Run("all time fields are in Validate method", func(*testing.T) {
		// Test that Validate() doesn't return an error when all times are equal
		testConfig := upgrade.Config{}
		configValue := reflect.ValueOf(&testConfig).Elem()

		testTime := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
		for _, fieldName := range timeFields {
			field := configValue.FieldByName(fieldName)
			require.True(field.IsValid(), "Field %s should exist", fieldName)
			field.Set(reflect.ValueOf(testTime))
		}

		// Set duration fields to valid values
		for _, fieldName := range durationFields {
			field := configValue.FieldByName(fieldName)
			if field.IsValid() {
				field.Set(reflect.ValueOf(time.Minute))
			}
		}

		// If all times are equal, Validate should pass
		require.NoError(testConfig.Validate(), "Validate should pass when all upgrade times are equal")

		// Now test that Validate catches out-of-order times
		// Set the last time field to before the first
		if len(timeFields) >= 2 {
			lastField := configValue.FieldByName(timeFields[len(timeFields)-1])
			lastField.Set(reflect.ValueOf(testTime.Add(-time.Hour)))
			err := testConfig.Validate()
			require.ErrorIs(err, upgrade.ErrInvalidUpgradeTimes, "Validate should fail when upgrade times are out of order")
		}
	})

	t.Run("all time fields are set in network configs", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			config upgrade.Config
		}{
			{"Mainnet", upgrade.Mainnet},
			{"Fuji", upgrade.Fuji},
			{"Default", upgrade.Default},
		} {
			t.Run(tc.name, func(*testing.T) {
				configValue := reflect.ValueOf(tc.config)
				for _, fieldName := range timeFields {
					field := configValue.FieldByName(fieldName)
					require.True(field.IsValid(), "%s.%s should exist", tc.name, fieldName)
					timeValue := field.Interface().(time.Time)
					require.False(timeValue.IsZero(), "%s.%s should be set to a non-zero time", tc.name, fieldName)
				}
			})
		}
	})

	t.Run("all Fork constants are stringifiable", func(*testing.T) {
		// Check that each fork can produce a valid string representation
		for fork := NoUpgrades; fork <= Latest; fork++ {
			forkName := fork.String()
			require.NotEqual("Unknown", forkName, "Fork %d should be stringifiable", fork)
		}
	})

	t.Run("upgradetest Fork constants match config fields", func(*testing.T) {
		// For each time field in upgrade.Config (except Apricot phases and special cases),
		// there should be a corresponding Fork constant
		for _, fieldName := range timeFields {
			upgradeName := strings.TrimSuffix(fieldName, "Time")
			// Skip Apricot variants as they have complex naming
			if strings.HasPrefix(upgradeName, "Apricot") {
				continue
			}
			// Skip NoUpgrades as it's a special case
			if upgradeName == "NoUpgrades" {
				continue
			}

			fork := FromString(upgradeName)
			require.NotEqual(Fork(-1), fork, "Fork constant for %s should exist", upgradeName)
			require.GreaterOrEqual(fork, Fork(0), "Fork constant for %s should be valid", upgradeName)
			require.LessOrEqual(fork, Latest, "Fork constant for %s should be <= Latest", upgradeName)
		}
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
						require.Equal(reflect.TypeOf(time.Time{}), field.Type(),
							"Field %s should be time.Time type", fieldName)
						timeVal := field.Interface().(time.Time)
						require.True(timeVal.Equal(testTime),
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
						require.False(timeVal.Equal(testTime),
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
	require := require.New(t)

	configType := reflect.TypeOf(upgrade.Config{})

	t.Run("time fields end with Time suffix", func(*testing.T) {
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			if field.Type == reflect.TypeOf(time.Time{}) {
				require.True(
					strings.HasSuffix(field.Name, "Time"),
					"time.Time field %s should have 'Time' suffix",
					field.Name,
				)
			}
		}
	})

	t.Run("duration fields end with Duration or Height suffix", func(*testing.T) {
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			if field.Type == reflect.TypeOf(time.Duration(0)) {
				require.True(
					strings.HasSuffix(field.Name, "Duration") || strings.HasSuffix(field.Name, "Height"),
					"time.Duration field %s should have 'Duration' or 'Height' suffix",
					field.Name,
				)
			}
		}
	})

	t.Run("json tags use camelCase", func(*testing.T) {
		for i := 0; i < configType.NumField(); i++ {
			field := configType.Field(i)
			jsonTag := field.Tag.Get("json")
			if jsonTag != "" {
				// Extract just the name part (before any commas)
				jsonName := strings.Split(jsonTag, ",")[0]
				require.NotEmpty(jsonName, "Field %s should have non-empty json tag", field.Name)
				require.True(
					len(jsonName) > 0 && jsonName[0] >= 'a' && jsonName[0] <= 'z',
					"Field %s json tag '%s' should start with lowercase letter",
					field.Name,
					jsonName,
				)
			}
		}
	})
}

// TestForkStringCompleteness ensures all Fork constants have String() cases.
func TestForkStringCompleteness(t *testing.T) {
	require := require.New(t)

	// Test that no fork returns "Unknown" except when explicitly set to an invalid value
	for fork := NoUpgrades; fork <= Latest+1; fork++ {
		forkStr := fork.String()
		if fork <= Latest {
			require.NotEqual("Unknown", forkStr, "Fork %d should not return 'Unknown'", fork)
		} else {
			// Forks beyond Latest should return "Unknown"
			require.Equal("Unknown", forkStr, "Fork %d (beyond Latest) should return 'Unknown'", fork)
		}
	}
}

// TestFromString ensures FromString correctly maps all fork names to their constants.
func TestFromString(t *testing.T) {
	require := require.New(t)

	t.Run("valid fork names", func(*testing.T) {
		// Test that all valid fork constants can be converted to string and back
		for fork := NoUpgrades; fork <= Latest; fork++ {
			forkStr := fork.String()
			parsedFork := FromString(forkStr)
			require.Equal(fork, parsedFork, "FromString(%q) should return %d, got %d", forkStr, fork, parsedFork)
		}
	})

	t.Run("invalid fork names", func(*testing.T) {
		invalidNames := []string{
			"",
			"Unknown",
			"InvalidFork",
			"banff",           // lowercase
			"GRANITE",         // uppercase
			" Helicon",        // leading space
			"Helicon ",        // trailing space
			"ApricotPhase999", // non-existent phase
		}

		for _, name := range invalidNames {
			fork := FromString(name)
			require.Equal(Fork(-1), fork, "FromString(%q) should return -1 for invalid name", name)
		}
	})
}
