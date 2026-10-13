// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/ava-labs/avalanchego/upgrade"
)

var (
	ErrUpgradeBeforeFork       = errors.New("upgrade override changes an activation before the fork time")
	ErrUpgradeParameterChanged = errors.New("upgrade override changes a non-time parameter")
)

// VerifyUpgradeOverride enforces that [override] only reschedules
// activations that, both before and after the override, happen at or after
// [forkTime]. Anything else would change the rules of pre-fork blocks.
func VerifyUpgradeOverride(defaults, override upgrade.Config, forkTime time.Time) error {
	var (
		defaultValue  = reflect.ValueOf(defaults)
		overrideValue = reflect.ValueOf(override)
		configType    = defaultValue.Type()
	)
	for i := range configType.NumField() {
		var (
			name = configType.Field(i).Name
			d    = defaultValue.Field(i).Interface()
			o    = overrideValue.Field(i).Interface()
		)
		defaultTime, isTime := d.(time.Time)
		if !isTime {
			if !reflect.DeepEqual(d, o) {
				return fmt.Errorf("%w: %s", ErrUpgradeParameterChanged, name)
			}
			continue
		}

		overrideTime := o.(time.Time)
		if defaultTime.Equal(overrideTime) {
			continue
		}
		if defaultTime.Before(forkTime) || overrideTime.Before(forkTime) {
			return fmt.Errorf("%w: %s from %s to %s (fork time %s)",
				ErrUpgradeBeforeFork, name, defaultTime, overrideTime, forkTime)
		}
	}
	return nil
}
