// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import "time"

type Times struct {
	// Time of the AP3 network upgrade
	ApricotPhase3Time time.Time

	// Time of the AP5 network upgrade
	ApricotPhase5Time time.Time

	// Time of the Banff network upgrade
	BanffTime time.Time

	// Time of the Cortina network upgrade
	CortinaTime time.Time

	// Time of the Durango network upgrade
	DurangoTime time.Time

	// Time of the E network upgrade
	EUpgradeTime time.Time
}

func (t *Times) IsApricotPhase3Activated(timestamp time.Time) bool {
	return !timestamp.Before(t.ApricotPhase3Time)
}

func (t *Times) IsApricotPhase5Activated(timestamp time.Time) bool {
	return !timestamp.Before(t.ApricotPhase5Time)
}

func (t *Times) IsBanffActivated(timestamp time.Time) bool {
	return !timestamp.Before(t.BanffTime)
}

func (t *Times) IsCortinaActivated(timestamp time.Time) bool {
	return !timestamp.Before(t.CortinaTime)
}

func (t *Times) IsDurangoActivated(timestamp time.Time) bool {
	return !timestamp.Before(t.DurangoTime)
}

func (t *Times) IsEActivated(timestamp time.Time) bool {
	return !timestamp.Before(t.EUpgradeTime)
}
