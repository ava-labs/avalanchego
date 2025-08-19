// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/subnet-evm/plugin/evm/validators/interfaces"

	stateinterfaces "github.com/ava-labs/subnet-evm/plugin/evm/validators/state/interfaces"
)

type RLocker interface {
	RLock()
	RUnlock()
}

type lockedReader struct {
	manager interfaces.Manager
	lock    RLocker
}

func NewLockedValidatorReader(
	manager interfaces.Manager,
	lock RLocker,
) interfaces.ValidatorReader {
	return &lockedReader{
		lock:    lock,
		manager: manager,
	}
}

// GetValidatorAndUptime returns the calculated uptime of the validator specified by validationID
// and the last updated time.
// GetValidatorAndUptime holds the lock while performing the operation and can be called concurrently.
func (l *lockedReader) GetValidatorAndUptime(validationID ids.ID) (stateinterfaces.Validator, time.Duration, time.Time, error) {
	l.lock.RLock()
	defer l.lock.RUnlock()

	vdr, err := l.manager.GetValidator(validationID)
	if err != nil {
		return stateinterfaces.Validator{}, 0, time.Time{}, fmt.Errorf("failed to get validator: %w", err)
	}

	uptime, lastUpdated, err := l.manager.CalculateUptime(vdr.NodeID)
	if err != nil {
		return stateinterfaces.Validator{}, 0, time.Time{}, fmt.Errorf("failed to get uptime: %w", err)
	}

	return vdr, uptime, lastUpdated, nil
}
