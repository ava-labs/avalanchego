// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/config"
)

type Phase int

// Camino phases must go in consecutive order
const (
	PhaseApricot5 Phase = 0 // avax
	PhaseBanff    Phase = 1 // avax, included into Sunrise phase
	PhaseSunrise  Phase = 1
	PhaseAthens   Phase = 2
	PhaseCortina  Phase = 3 // avax, included into Berlin phase
	PhaseBerlin   Phase = 3
	PhaseCairo    Phase = 4
)

const (
	PhaseFirst = PhaseSunrise
	PhaseLast  = PhaseBerlin
)

func PhaseTime(t *testing.T, phase Phase, cfg *config.Config) time.Time {
	switch phase {
	case PhaseSunrise:
		return cfg.AthensPhaseTime.Add(-time.Second)
	case PhaseAthens:
		return cfg.AthensPhaseTime
	case PhaseBerlin:
		return cfg.BerlinPhaseTime
	case PhaseCairo:
		return cfg.CairoPhaseTime
	}
	require.FailNow(t, "unknown phase")
	return time.Time{}
}

func PhaseName(t *testing.T, phase Phase) string {
	switch phase {
	case PhaseSunrise:
		return "SunrisePhase"
	case PhaseAthens:
		return "AthensPhase"
	case PhaseBerlin:
		return "BerlinPhase"
	case PhaseCairo:
		return "CairoPhase"
	}
	require.FailNow(t, "unknown phase")
	return ""
}
