// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

// AvalancheRules describes the Avalanche upgrades active for a block.
type AvalancheRules struct {
	IsApricotPhase1, IsApricotPhase2, IsApricotPhase3, IsApricotPhase4, IsApricotPhase5 bool
	IsApricotPhasePre6, IsApricotPhase6, IsApricotPhasePost6                            bool
	IsBanff                                                                             bool
	IsCortina                                                                           bool
	IsDurango                                                                           bool
	IsEtna                                                                              bool
	IsFortuna                                                                           bool
	IsGranite                                                                           bool
	IsHelicon                                                                           bool
	IsIgloo                                                                             bool
}

func (a AvalancheRules) IsGraniteActivated() bool {
	return a.IsGranite
}

func (a AvalancheRules) IsDurangoActivated() bool {
	return a.IsDurango
}
