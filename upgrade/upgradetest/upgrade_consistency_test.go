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

	t.Run("all time fields have activation methods", func(t *testing.T) {
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

	t.Run("all time fields are in Validate method", func(t *testing.T) {
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
			require.Error(testConfig.Validate(), "Validate should fail when upgrade times are out of order")
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
			t.Run(tc.name, func(t *testing.T) {
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

	t.Run("upgradetest Fork constants match config fields", func(t *testing.T) {
		// For each time field in upgrade.Config (except Apricot phases and special cases),
		// there should be a corresponding Fork constant
		expectedForks := make(map[string]bool)
		for _, fieldName := range timeFields {
			upgradeName := strings.TrimSuffix(fieldName, "Time")
			// Skip Apricot variants as they have complex naming
			if !strings.HasPrefix(upgradeName, "Apricot") {
				expectedForks[upgradeName] = false
			}
		}

		// Check that each fork can be stringified
		for fork := NoUpgrades; fork <= Latest; fork++ {
			forkName := fork.String()
			if forkName == "NoUpgrades" {
				continue
			}

			if _, exists := expectedForks[forkName]; exists {
				expectedForks[forkName] = true
			}
		}

		// Verify all expected forks were found
		for forkName, found := range expectedForks {
			require.True(found, "Fork constant for %s should exist and be stringifiable", forkName)
		}
	})

	t.Run("upgradetest.SetTimesTo handles all forks", func(t *testing.T) {
		// This test ensures SetTimesTo properly handles each fork
		testTime := time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)

		for fork := ApricotPhase1; fork <= Latest; fork++ {
			forkName := fork.String()

			t.Run(forkName, func(t *testing.T) {
				config := upgrade.Config{}
				SetTimesTo(&config, fork, testTime)

				// Verify that at least this fork's time is set
				configValue := reflect.ValueOf(config)
				var foundMatchingTime bool
				for i := 0; i < configValue.NumField(); i++ {
					field := configValue.Field(i)
					if field.Type() == reflect.TypeOf(time.Time{}) {
						timeVal := field.Interface().(time.Time)
						if timeVal.Equal(testTime) {
							foundMatchingTime = true
							break
						}
					}
				}
				require.True(foundMatchingTime, "SetTimesTo should set at least one time field for fork %s", forkName)
			})
		}
	})

	t.Run("Latest fork is properly defined", func(t *testing.T) {
		// Latest should be a valid fork constant
		require.NotEqual(NoUpgrades, Latest, "Latest should not be NoUpgrades")
		require.NotEqual("Unknown", Latest.String(), "Latest should have a valid string representation")

		// Latest should be >= all other named forks
		require.True(Latest >= Granite, "Latest should be >= Granite")
	})
}

// TestUpgradeFieldNaming ensures consistent naming conventions across the codebase.
func TestUpgradeFieldNaming(t *testing.T) {
	require := require.New(t)

	configType := reflect.TypeOf(upgrade.Config{})

	t.Run("time fields end with Time suffix", func(t *testing.T) {
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

	t.Run("duration fields end with Duration or Height suffix", func(t *testing.T) {
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

	t.Run("json tags use camelCase", func(t *testing.T) {
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
