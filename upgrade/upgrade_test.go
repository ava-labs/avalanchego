// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidDefaultUpgrades(t *testing.T) {
	for _, upgradeTest := range []struct {
		name    string
		upgrade Config
	}{
		{
			name:    "Default",
			upgrade: Default,
		},
		{
			name:    "Fuji",
			upgrade: Fuji,
		},
		{
			name:    "Mainnet",
			upgrade: Mainnet,
		},
	} {
		t.Run(upgradeTest.name, func(t *testing.T) {
			require := require.New(t)
			require.NoError(upgradeTest.upgrade.Validate())
		})
	}
}

func TestInvalidUpgrade(t *testing.T) {
	firstUpgradeTime := time.Now()
	invalidSecondUpgradeTime := firstUpgradeTime.Add(-1 * time.Second)
	upgrade := Config{
		ApricotPhase1Time: firstUpgradeTime,
		ApricotPhase2Time: invalidSecondUpgradeTime,
	}
	err := upgrade.Validate()
	require.ErrorIs(t, err, ErrInvalidUpgradeTimes)
}

func TestValidUpgradeTimeOrdering(t *testing.T) {
	configType := reflect.TypeOf(Config{})
	var timeFields []string

	for i := 0; i < configType.NumField(); i++ {
		field := configType.Field(i)
		if field.Type == reflect.TypeOf(time.Time{}) {
			timeFields = append(timeFields, field.Name)
		}
	}

	// Test that Validate() doesn't return an error when all times are equal
	testConfig := Config{}
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
		require.ErrorIs(t, err, ErrInvalidUpgradeTimes, "Validate should fail when upgrade times are out of order")
	}
}
