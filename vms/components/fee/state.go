// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

type State struct {
	Capacity Gas
	Excess   Gas
}

func (s State) AdvanceTime(c Config, duration uint64) State {
	return State{
		Capacity: min(
			s.Capacity.AddPerSecond(c.MaxGasPerSecond, duration),
			c.MaxGasCapacity,
		),
		Excess: s.Excess.SubPerSecond(c.TargetGasPerSecond, duration),
	}
}
