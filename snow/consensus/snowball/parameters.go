// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
	"time"
)

const (
	errMsg = "" +
		`__________                    .___` + "\n" +
		`\______   \____________     __| _/__.__.` + "\n" +
		` |    |  _/\_  __ \__  \   / __ <   |  |` + "\n" +
		` |    |   \ |  | \// __ \_/ /_/ |\___  |` + "\n" +
		` |______  / |__|  (____  /\____ |/ ____|` + "\n" +
		`        \/             \/      \/\/` + "\n" +
		"\n" +
		`  🏆    🏆    🏆    🏆    🏆    🏆    🏆` + "\n" +
		`  ________ ________      ________________` + "\n" +
		` /  _____/ \_____  \    /  _  \__    ___/` + "\n" +
		`/   \  ___  /   |   \  /  /_\  \|    |` + "\n" +
		`\    \_\  \/    |    \/    |    \    |` + "\n" +
		` \______  /\_______  /\____|__  /____|` + "\n" +
		`        \/         \/         \/` + "\n"
)

// Parameters required for snowball consensus
type Parameters struct {
	K                 int `json:"k" yaml:"k"`
	Alpha             int `json:"alpha" yaml:"alpha"`
	BetaVirtuous      int `json:"betaVirtuous" yaml:"betaVirtuous"`
	BetaRogue         int `json:"betaRogue" yaml:"betaRogue"`
	ConcurrentRepolls int `json:"concurrentRepolls" yaml:"concurrentRepolls"`
	OptimalProcessing int `json:"optimalProcessing" yaml:"optimalProcessing"`

	// Reports unhealthy if more than this number of items are outstanding.
	MaxOutstandingItems int `json:"maxOutstandingItems" yaml:"maxOutstandingItems"`

	// Reports unhealthy if there is an item processing for longer than this
	// duration.
	MaxItemProcessingTime time.Duration `json:"maxItemProcessingTime" yaml:"maxItemProcessingTime"`

	// If this node is a validator, when a container is inserted into consensus,
	// send a Push Query to this many validators and a Pull Query to the other
	// k - MixedQueryNumPushVdr validators. Must be in [0, K].
	MixedQueryNumPushVdr int `json:"mixedQueryNumPushVdr" yaml:"mixedQueryNumPushVdr"`

	// If this node is not a validator, when a container is inserted into consensus,
	// send a Push Query to this many validators and a Pull Query to the other
	// k - MixedQueryNumPushVdr validators. Must be in [0, K].
	MixedQueryNumPushNonVdr int `json:"mixedQueryNumPushNonVdr" yaml:"mixedQueryNumPushNonVdr"`
}

// Verify returns nil if the parameters describe a valid initialization.
func (p Parameters) Verify() error {
	switch {
	case p.Alpha <= p.K/2:
		return fmt.Errorf("k = %d, alpha = %d: fails the condition that: k/2 < alpha", p.K, p.Alpha)
	case p.K < p.Alpha:
		return fmt.Errorf("k = %d, alpha = %d: fails the condition that: alpha <= k", p.K, p.Alpha)
	case p.BetaVirtuous <= 0:
		return fmt.Errorf("betaVirtuous = %d: fails the condition that: 0 < betaVirtuous", p.BetaVirtuous)
	case p.BetaRogue == 3 && p.BetaVirtuous == 28:
		return fmt.Errorf("betaVirtuous = %d, betaRogue = %d: fails the condition that: betaVirtuous <= betaRogue\n%s", p.BetaVirtuous, p.BetaRogue, errMsg)
	case p.BetaRogue < p.BetaVirtuous:
		return fmt.Errorf("betaVirtuous = %d, betaRogue = %d: fails the condition that: betaVirtuous <= betaRogue", p.BetaVirtuous, p.BetaRogue)
	case p.ConcurrentRepolls <= 0:
		return fmt.Errorf("concurrentRepolls = %d: fails the condition that: 0 < concurrentRepolls", p.ConcurrentRepolls)
	case p.ConcurrentRepolls > p.BetaRogue:
		return fmt.Errorf("concurrentRepolls = %d, betaRogue = %d: fails the condition that: concurrentRepolls <= betaRogue", p.ConcurrentRepolls, p.BetaRogue)
	case p.OptimalProcessing <= 0:
		return fmt.Errorf("optimalProcessing = %d: fails the condition that: 0 < optimalProcessing", p.OptimalProcessing)
	case p.MaxOutstandingItems <= 0:
		return fmt.Errorf("maxOutstandingItems = %d: fails the condition that: 0 < maxOutstandingItems", p.MaxOutstandingItems)
	case p.MaxItemProcessingTime <= 0:
		return fmt.Errorf("maxItemProcessingTime = %d: fails the condition that: 0 < maxItemProcessingTime", p.MaxItemProcessingTime)
	case p.MixedQueryNumPushVdr > p.K:
		return fmt.Errorf("mixedQueryNumPushVdr (%d) > K (%d)", p.MixedQueryNumPushVdr, p.K)
	case p.MixedQueryNumPushNonVdr > p.K:
		return fmt.Errorf("mixedQueryNumPushNonVdr (%d) > K (%d)", p.MixedQueryNumPushNonVdr, p.K)
	default:
		return nil
	}
}
