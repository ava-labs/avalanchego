// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"pgregory.net/rapid"
)

// errNoCommits means that a tx type got so many fair attempts with a verdict,
// and none committed, that a node that takes valid txs of the type would
// almost never do that.
var errNoCommits = errors.New("tx type never commits")

// A commitTally counts, by tx type, the fair attempts that got a verdict, the
// ones that committed, and the reasons the others were refused or dropped. An
// attempt is fair when its tx is new and unmutated, the wallet built it from
// the payer's own funds, and each reference is an active entity of its kind
// (refID). The zero value is ready to use.
type commitTally struct {
	fair     map[string]int
	commits  map[string]int
	refusals map[string]map[string]int // by tx type, then reason
}

// add counts a fair attempt of txType that ended in result. reason is the
// check that refused or dropped it (runner.decided). An attempt with no
// verdict is not counted: it may still commit.
func (c *commitTally) add(txType string, result outcome, reason string) {
	switch result {
	case committed, refused, dropped:
	default:
		return
	}

	if c.fair == nil {
		c.fair = make(map[string]int)
		c.commits = make(map[string]int)
		c.refusals = make(map[string]map[string]int)
	}
	c.fair[txType]++
	if result == committed {
		c.commits[txType]++
		return
	}

	if c.refusals[txType] == nil {
		c.refusals[txType] = make(map[string]int)
	}
	c.refusals[txType][reason]++
}

// check returns errNoCommits for a tx type in floors that committed none of
// its n fair attempts, while a node that commits each one with at least the
// type's floor would do that with a chance below 1e-9: (1-floor)^n < 1e-9.
// Types not in floors are not judged.
//
// Guarantee: a node takes a valid tx. A fair attempt builds a tx from the
// model's live state the way a user would, so baseline runs commit a measured
// share of them (checkCommits). Nothing here predicts one tx's verdict: one
// refusal proves nothing, only a run of them far longer than the baseline
// allows. The chance that the first k attempts all fail only shrinks as k
// grows, so checking after every op does not add up false positives.
func (c *commitTally) check(floors map[string]float64) error {
	const falsePositive = 1e-9

	for _, txType := range slices.Sorted(maps.Keys(c.fair)) {
		floor, ok := floors[txType]
		n := c.fair[txType]
		if !ok || c.commits[txType] > 0 || float64(n)*math.Log1p(-floor) >= math.Log(falsePositive) {
			continue
		}

		return fmt.Errorf("%w: %s committed none of %d fair attempts, a chance below %g at its commit rate floor of %g; refusals: %s", errNoCommits, txType, n, falsePositive, floor, c.describeRefusals(txType))
	}

	return nil
}

// describeRefusals lists txType's refusal reasons with their counts, the most
// common first.
func (c *commitTally) describeRefusals(txType string) string {
	counts := c.refusals[txType]
	reasons := slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), strings.Compare(a, b))
	})
	parts := make([]string, len(reasons))
	for i, reason := range reasons {
		parts[i] = fmt.Sprintf("%s (%d)", reason, counts[reason])
	}

	return strings.Join(parts, "; ")
}

// checkCommits fails t if a tx type's fair attempts never commit, in this run
// or across the campaign (commitTally.check). It catches a node that refuses a
// valid tx: no op predicts a verdict, so no other check sees that.
//
// Each floor is half the lowest share of fair attempts that committed in a
// baseline run with at least 5 of them, rounded down to a multiple of 0.05,
// over 19 runs of 9 to 1765 ops (Stateful Fuzzing.md 3). Types that are not
// here are not judged: the disabled and proposer-only types never commit, and
// AddPermissionlessDelegatorTx ranged from 33% to 91% by run, as the
// delegation cap filled and freed, too uneven to bound. The comment beside a
// floor below 0.5 gives that lowest run.
func (r *runner) checkCommits(t *rapid.T) {
	floors := map[string]float64{
		"BaseTx":                          0.5,
		"CreateSubnetTx":                  0.5,
		"CreateChainTx":                   0.5,
		"ExportTx":                        0.5,
		"ImportTx":                        0.5,
		"TransferSubnetOwnershipTx":       0.5,
		"IncreaseL1ValidatorBalanceTx":    0.5,
		"DisableL1ValidatorTx":            0.5,
		"SetAutoRenewedValidatorConfigTx": 0.45, // 30 of 31
		"AddAutoRenewedValidatorTx":       0.3,  // 10 of 15
		"AddPermissionlessValidatorTx":    0.3,  // 13 of 21
		"ConvertSubnetToL1Tx":             0.3,  // 8 of 13
		"RemoveSubnetValidatorTx":         0.3,  // 3 of 5
		"AddSubnetValidatorTx":            0.2,  // 5 of 11
	}

	if err := r.commits.check(floors); err != nil {
		r.fail(t, "commits", err)
	}
	if r.campaign == nil {
		return
	}

	if err := r.campaign.check(floors); err != nil {
		r.fail(t, "campaign commits", err)
	}
}

// refID draws a reference to an entity of the kind that active holds, by the
// ID rule (drawID), and marks g's attempt unfair if the draw is not one of
// active.
func refID[K id](g *generator, label string, active []K, seen *seenSet[K]) K {
	k := drawID(g.t, g.r.ev, label, active, seen)
	if !slices.Contains(active, k) {
		g.offKind = true
	}

	return k
}
