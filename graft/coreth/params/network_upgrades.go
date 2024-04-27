// (c) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

func (c *ChainConfig) forkOrder() []fork {
	return []fork{
		{name: "apricotPhase1BlockTimestamp", timestamp: c.ApricotPhase1BlockTimestamp},
		{name: "apricotPhase2BlockTimestamp", timestamp: c.ApricotPhase2BlockTimestamp},
		{name: "apricotPhase3BlockTimestamp", timestamp: c.ApricotPhase3BlockTimestamp},
		{name: "apricotPhase4BlockTimestamp", timestamp: c.ApricotPhase4BlockTimestamp},
		{name: "apricotPhase5BlockTimestamp", timestamp: c.ApricotPhase5BlockTimestamp},
		{name: "apricotPhasePre6BlockTimestamp", timestamp: c.ApricotPhasePre6BlockTimestamp},
		{name: "apricotPhase6BlockTimestamp", timestamp: c.ApricotPhase6BlockTimestamp},
		{name: "apricotPhasePost6BlockTimestamp", timestamp: c.ApricotPhasePost6BlockTimestamp},
		{name: "banffBlockTimestamp", timestamp: c.BanffBlockTimestamp},
		{name: "cortinaBlockTimestamp", timestamp: c.CortinaBlockTimestamp},
		{name: "durangoBlockTimestamp", timestamp: c.DurangoBlockTimestamp},
	}
}

type AvalancheRules struct {
	IsApricotPhase1, IsApricotPhase2, IsApricotPhase3, IsApricotPhase4, IsApricotPhase5 bool
	IsApricotPhasePre6, IsApricotPhase6, IsApricotPhasePost6                            bool
	IsBanff                                                                             bool
	IsCortina                                                                           bool
	IsDurango                                                                           bool
}

func (c *ChainConfig) GetAvalancheRules(timestamp uint64) AvalancheRules {
	rules := AvalancheRules{}
	rules.IsApricotPhase1 = c.IsApricotPhase1(timestamp)
	rules.IsApricotPhase2 = c.IsApricotPhase2(timestamp)
	rules.IsApricotPhase3 = c.IsApricotPhase3(timestamp)
	rules.IsApricotPhase4 = c.IsApricotPhase4(timestamp)
	rules.IsApricotPhase5 = c.IsApricotPhase5(timestamp)
	rules.IsApricotPhasePre6 = c.IsApricotPhasePre6(timestamp)
	rules.IsApricotPhase6 = c.IsApricotPhase6(timestamp)
	rules.IsApricotPhasePost6 = c.IsApricotPhasePost6(timestamp)
	rules.IsBanff = c.IsBanff(timestamp)
	rules.IsCortina = c.IsCortina(timestamp)
	rules.IsDurango = c.IsDurango(timestamp)

	return rules
}
