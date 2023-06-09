// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"errors"
	"fmt"
	"time"
)

const (
	// MinPercentConnectedBuffer is the safety buffer for calculation of
	// MinPercentConnected. This increases the required percentage above
	// alpha/k. This value must be [0-1].
	// 0 means MinPercentConnected = alpha/k.
	// 1 means MinPercentConnected = 1 (fully connected).
	MinPercentConnectedBuffer = .2

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

var (
	DefaultParameters = Parameters{
		K:                     20,
		Alpha:                 15,
		BetaVirtuous:          15,
		BetaRogue:             20,
		ConcurrentRepolls:     4,
		OptimalProcessing:     10,
		MaxOutstandingItems:   256,
		MaxItemProcessingTime: 30 * time.Second,
	}

	ErrParametersInvalid = errors.New("parameters invalid")
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
}

// Verify returns nil if the parameters describe a valid initialization.
func (p Parameters) Verify() error {
	switch {
	case p.Alpha <= p.K/2:
		return fmt.Errorf("%w: k = %d, alpha = %d: fails the condition that: k/2 < alpha", ErrParametersInvalid, p.K, p.Alpha)
	case p.K < p.Alpha:
		return fmt.Errorf("%w: k = %d, alpha = %d: fails the condition that: alpha <= k", ErrParametersInvalid, p.K, p.Alpha)
	case p.BetaVirtuous <= 0:
		return fmt.Errorf("%w: betaVirtuous = %d: fails the condition that: 0 < betaVirtuous", ErrParametersInvalid, p.BetaVirtuous)
	case p.BetaRogue == 3 && p.BetaVirtuous == 28:
		return fmt.Errorf("%w: betaVirtuous = %d, betaRogue = %d: fails the condition that: betaVirtuous <= betaRogue\n%s", ErrParametersInvalid, p.BetaVirtuous, p.BetaRogue, errMsg)
	case p.BetaRogue < p.BetaVirtuous:
		return fmt.Errorf("%w: betaVirtuous = %d, betaRogue = %d: fails the condition that: betaVirtuous <= betaRogue", ErrParametersInvalid, p.BetaVirtuous, p.BetaRogue)
	case p.ConcurrentRepolls <= 0:
		return fmt.Errorf("%w: concurrentRepolls = %d: fails the condition that: 0 < concurrentRepolls", ErrParametersInvalid, p.ConcurrentRepolls)
	case p.ConcurrentRepolls > p.BetaRogue:
		return fmt.Errorf("%w: concurrentRepolls = %d, betaRogue = %d: fails the condition that: concurrentRepolls <= betaRogue", ErrParametersInvalid, p.ConcurrentRepolls, p.BetaRogue)
	case p.OptimalProcessing <= 0:
		return fmt.Errorf("%w: optimalProcessing = %d: fails the condition that: 0 < optimalProcessing", ErrParametersInvalid, p.OptimalProcessing)
	case p.MaxOutstandingItems <= 0:
		return fmt.Errorf("%w: maxOutstandingItems = %d: fails the condition that: 0 < maxOutstandingItems", ErrParametersInvalid, p.MaxOutstandingItems)
	case p.MaxItemProcessingTime <= 0:
		return fmt.Errorf("%w: maxItemProcessingTime = %d: fails the condition that: 0 < maxItemProcessingTime", ErrParametersInvalid, p.MaxItemProcessingTime)
	default:
		return nil
	}
}

func (p Parameters) MinPercentConnectedHealthy() float64 {
	alpha := p.Alpha
	k := p.K
	r := float64(alpha) / float64(k)
	return r*(1-MinPercentConnectedBuffer) + MinPercentConnectedBuffer
}
