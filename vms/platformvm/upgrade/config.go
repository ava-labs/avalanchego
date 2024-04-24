// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package upgrade

import "time"

type Config struct {
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

func (c *Config) IsApricotPhase3Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase3Time)
}

func (c *Config) IsApricotPhase5Activated(timestamp time.Time) bool {
	return !timestamp.Before(c.ApricotPhase5Time)
}

func (c *Config) IsBanffActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.BanffTime)
}

func (c *Config) IsCortinaActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.CortinaTime)
}

func (c *Config) IsDurangoActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.DurangoTime)
}

func (c *Config) IsEActivated(timestamp time.Time) bool {
	return !timestamp.Before(c.EUpgradeTime)
}
