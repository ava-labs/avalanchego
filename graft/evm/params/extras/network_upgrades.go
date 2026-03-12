// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extras

import (
	"fmt"
	"math/big"
	"reflect"
	"strconv"

	ethparams "github.com/ava-labs/libevm/params"
)

type NetworkUpgrades struct {
	// These must be embedded pointers to promote the names in the JSON.
	*CorethNetworkUpgrades
	*SubnetEVMNetworkUpgrades

	// Placeholder for EtnaTimestamp
	EtnaTimestamp *uint64 `json:"etnaTimestamp,omitempty"`
	// Fortuna has no effect on Subnet-EVM by itself, but is included for completeness.
	FortunaTimestamp *uint64 `json:"fortunaTimestamp,omitempty"`
	// Granite adds a millisecond timestamp, precompile updates, and P-Chain epochs
	GraniteTimestamp *uint64 `json:"graniteTimestamp,omitempty"`
	// Helicon is a placeholder for the next upgrade
	HeliconTimestamp *uint64 `json:"heliconTimestamp,omitempty"`
}

func (n *NetworkUpgrades) Equal(other *NetworkUpgrades) bool {
	return reflect.DeepEqual(n, other)
}

// Copy returns a copy of n with independent copies of the embedded pointer
// structs, so that mutating the copy's timestamp fields does not affect n.
func (n NetworkUpgrades) Copy() NetworkUpgrades {
	if n.CorethNetworkUpgrades != nil {
		cnu := *n.CorethNetworkUpgrades
		n.CorethNetworkUpgrades = &cnu
	}
	if n.SubnetEVMNetworkUpgrades != nil {
		snu := *n.SubnetEVMNetworkUpgrades
		n.SubnetEVMNetworkUpgrades = &snu
	}
	return n
}

func (n *NetworkUpgrades) Description() string {
	var banner string
	if n.CorethNetworkUpgrades != nil {
		banner += n.CorethNetworkUpgrades.Description()
	}
	if n.SubnetEVMNetworkUpgrades != nil {
		banner += n.SubnetEVMNetworkUpgrades.Description()
	}
	banner += fmt.Sprintf(" - Etna Timestamp:                   @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.12.0)\n", ptrToString(n.EtnaTimestamp))
	banner += fmt.Sprintf(" - Fortuna Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.13.0)\n", ptrToString(n.FortunaTimestamp))
	banner += fmt.Sprintf(" - Granite Timestamp:                @%-10v (https://github.com/ava-labs/avalanchego/releases/tag/v1.14.0)\n", ptrToString(n.GraniteTimestamp))
	banner += fmt.Sprintf(" - Helicon Timestamp:                @%-10v (Unscheduled)\n", ptrToString(n.HeliconTimestamp))
	return banner
}

func (n *NetworkUpgrades) CheckNetworkUpgradesCompatible(newcfg *NetworkUpgrades, time uint64) *ethparams.ConfigCompatError {
	if err := n.CorethNetworkUpgrades.checkNetworkUpgradesCompatible(newcfg.CorethNetworkUpgrades, time); err != nil {
		return err
	}
	if err := n.SubnetEVMNetworkUpgrades.checkNetworkUpgradesCompatible(newcfg.SubnetEVMNetworkUpgrades, time); err != nil {
		return err
	}

	if isForkTimestampIncompatible(n.EtnaTimestamp, newcfg.EtnaTimestamp, time) {
		return ethparams.NewTimestampCompatError("Etna fork block timestamp", n.EtnaTimestamp, newcfg.EtnaTimestamp)
	}
	if isForkTimestampIncompatible(n.FortunaTimestamp, newcfg.FortunaTimestamp, time) {
		return ethparams.NewTimestampCompatError("Fortuna fork block timestamp", n.FortunaTimestamp, newcfg.FortunaTimestamp)
	}
	if isForkTimestampIncompatible(n.GraniteTimestamp, newcfg.GraniteTimestamp, time) {
		return ethparams.NewTimestampCompatError("Granite fork block timestamp", n.GraniteTimestamp, newcfg.GraniteTimestamp)
	}
	if isForkTimestampIncompatible(n.HeliconTimestamp, newcfg.HeliconTimestamp, time) {
		return ethparams.NewTimestampCompatError("Helicon fork block timestamp", n.HeliconTimestamp, newcfg.HeliconTimestamp)
	}

	return nil
}

type Fork struct {
	Name      string
	Block     *big.Int // some go-ethereum forks use block numbers
	Timestamp *uint64  // Avalanche forks use timestamps
	Optional  bool     // if true, the fork may be nil and next fork is still allowed
}

func (n *NetworkUpgrades) ForkOrder() []Fork {
	var forks []Fork
	if n.CorethNetworkUpgrades != nil {
		forks = append(forks, n.CorethNetworkUpgrades.forkOrder()...)
		forks = append(forks,
			Fork{Name: "etnaTimestamp", Timestamp: n.EtnaTimestamp},
			Fork{Name: "fortunaTimestamp", Timestamp: n.FortunaTimestamp},
		)
	}
	if n.SubnetEVMNetworkUpgrades != nil {
		forks = append(forks, n.SubnetEVMNetworkUpgrades.forkOrder()...)
		forks = append(forks,
			Fork{Name: "etnaTimestamp", Timestamp: n.EtnaTimestamp},
			Fork{Name: "fortunaTimestamp", Timestamp: n.FortunaTimestamp, Optional: true},
		)
	}
	forks = append(forks,
		Fork{Name: "graniteTimestamp", Timestamp: n.GraniteTimestamp},
		Fork{Name: "heliconTimestamp", Timestamp: n.HeliconTimestamp},
	)
	return forks
}

// checkForks validates that the fork timestamps in the provided ordered list
// are non-decreasing, returning errUnsupportedForkOrdering if any fork has a
// timestamp earlier than the preceding non-nil fork.
func checkForks(forks []Fork) error {
	var lastTimestamp *uint64
	for _, fork := range forks {
		if fork.Timestamp == nil {
			continue
		}
		if lastTimestamp != nil && *fork.Timestamp < *lastTimestamp {
			return ErrUnsupportedForkOrdering
		}
		lastTimestamp = fork.Timestamp
	}
	return nil
}

// isForkTimestampIncompatible returns true if a fork scheduled at timestamp s1
// cannot be rescheduled to timestamp s2 because head is already past the fork.
func isForkTimestampIncompatible(s1, s2 *uint64, head uint64) bool {
	return (isTimestampForked(s1, head) || isTimestampForked(s2, head)) && !configTimestampEqual(s1, s2)
}

// isTimestampForked returns whether a fork scheduled at timestamp s is active
// at the given head timestamp.
func isTimestampForked(s *uint64, head uint64) bool {
	if s == nil {
		return false
	}
	return *s <= head
}

func configTimestampEqual(x, y *uint64) bool {
	if x == nil {
		return y == nil
	}
	if y == nil {
		return x == nil
	}
	return *x == *y
}

func ptrToString(val *uint64) string {
	if val == nil {
		return "nil"
	}
	return strconv.FormatUint(*val, 10)
}

// IsCortina returns whether [time] represents a block
// with a timestamp after the Cortina upgrade time.
// For Subnet-EVM chains, IsCortina corresponds to IsSubnetEVM, which is a
// catch-all that activates all upgrades at Cortina and below simultaneously.
func (n NetworkUpgrades) IsCortina(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.CortinaBlockTimestamp, time)
	}
	if n.SubnetEVMNetworkUpgrades != nil {
		return isTimestampForked(n.SubnetEVMNetworkUpgrades.SubnetEVMTimestamp, time)
	}
	return false
}

// IsApricotPhase1 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 1 upgrade time.
func (n NetworkUpgrades) IsApricotPhase1(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase1BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhase2 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 2 upgrade time.
func (n NetworkUpgrades) IsApricotPhase2(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase2BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhase3 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 3 upgrade time.
func (n NetworkUpgrades) IsApricotPhase3(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase3BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhase4 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 4 upgrade time.
func (n NetworkUpgrades) IsApricotPhase4(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase4BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhase5 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 5 upgrade time.
func (n NetworkUpgrades) IsApricotPhase5(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase5BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhasePre6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase Pre 6 upgrade time.
func (n NetworkUpgrades) IsApricotPhasePre6(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhasePre6BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhase6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 upgrade time.
func (n NetworkUpgrades) IsApricotPhase6(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhase6BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsApricotPhasePost6 returns whether [time] represents a block
// with a timestamp after the Apricot Phase 6 Post upgrade time.
func (n NetworkUpgrades) IsApricotPhasePost6(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.ApricotPhasePost6BlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsBanff returns whether [time] represents a block
// with a timestamp after the Banff upgrade time.
func (n NetworkUpgrades) IsBanff(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.BanffBlockTimestamp, time)
	}
	return n.IsCortina(time)
}

// IsDurango returns whether [time] represents a block
// with a timestamp after the Durango upgrade time.
func (n NetworkUpgrades) IsDurango(time uint64) bool {
	if n.CorethNetworkUpgrades != nil {
		return isTimestampForked(n.CorethNetworkUpgrades.DurangoBlockTimestamp, time)
	}
	if n.SubnetEVMNetworkUpgrades != nil {
		return isTimestampForked(n.SubnetEVMNetworkUpgrades.DurangoTimestamp, time)
	}
	return false
}

// IsEtna returns whether [time] represents a block
// with a timestamp after the Etna upgrade time.
func (n NetworkUpgrades) IsEtna(time uint64) bool {
	return isTimestampForked(n.EtnaTimestamp, time)
}

// IsFortuna returns whether [time] represents a block
// with a timestamp after the Fortuna upgrade time.
func (n NetworkUpgrades) IsFortuna(time uint64) bool {
	return isTimestampForked(n.FortunaTimestamp, time)
}

// IsGranite returns whether [time] represents a block
// with a timestamp after the Granite upgrade time.
func (n NetworkUpgrades) IsGranite(time uint64) bool {
	return isTimestampForked(n.GraniteTimestamp, time)
}

// IsHelicon returns whether [time] represents a block
// with a timestamp after the Helicon upgrade time.
func (n NetworkUpgrades) IsHelicon(time uint64) bool {
	return isTimestampForked(n.HeliconTimestamp, time)
}

func (n *NetworkUpgrades) GetAvalancheRules(time uint64) AvalancheRules {
	return AvalancheRules{
		IsApricotPhase1:     n.IsApricotPhase1(time),
		IsApricotPhase2:     n.IsApricotPhase2(time),
		IsApricotPhase3:     n.IsApricotPhase3(time),
		IsApricotPhase4:     n.IsApricotPhase4(time),
		IsApricotPhase5:     n.IsApricotPhase5(time),
		IsApricotPhasePre6:  n.IsApricotPhasePre6(time),
		IsApricotPhase6:     n.IsApricotPhase6(time),
		IsApricotPhasePost6: n.IsApricotPhasePost6(time),
		IsBanff:             n.IsBanff(time),
		IsCortina:           n.IsCortina(time),
		IsDurango:           n.IsDurango(time),
		IsEtna:              n.IsEtna(time),
		IsFortuna:           n.IsFortuna(time),
		IsGranite:           n.IsGranite(time),
		IsHelicon:           n.IsHelicon(time),
	}
}
