// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
)

const (
	pChainInvariantInterval = 5 * time.Second
	pChainInvariantTimeout  = 10 * time.Second

	// Every transaction issued during a node outage stays unresolved until the
	// node returns, so the bound must hold well over an hour of issuance.
	maxTrackedTxs = 4096
	// Each round checks a batch concurrently so a stalled node cannot consume
	// another transaction's budget. The batch must clear the issuance rate even
	// when both round budgets are exhausted, or the backlog only grows.
	maxCheckedTxs = 32
	// A transaction every node reports as dropped or unknown is resolved as
	// dropped after this many consecutive checks. One check is not enough:
	// while nodes prefer a competing block, a transaction in the losing block
	// is reported unknown everywhere until that block is rejected and the
	// transaction returns to the mempool.
	droppedChecks = 2
)

// pChainMonitor checks network-wide P-Chain invariants independently of
// worker execution: accepted history never regresses, nodes agree on state at
// a common height, and recorded transactions remain consistent while tracked.
type pChainMonitor struct {
	nodes       []pchain.Node
	log         logging.Logger
	nodeHeights map[ids.NodeID]uint64

	// Workers hand transactions to an inbox that each round drains, so the
	// tracked-set bound also bounds the inbox. The monitor owns all retained
	// history.
	pendingLock sync.Mutex
	pending     []pChainTx

	// txs holds unresolved transactions in the order they will next be checked.
	txs []*pChainTx
}

func newPChainMonitor(nodes []pchain.Node, log logging.Logger) *pChainMonitor {
	return &pChainMonitor{
		nodes:       nodes,
		log:         log,
		nodeHeights: make(map[ids.NodeID]uint64, len(nodes)),
	}
}

func (m *pChainMonitor) run(ctx context.Context) {
	for ctx.Err() == nil {
		m.checkNetwork(ctx)
		m.checkTransactions(ctx)
		timer := time.NewTimer(pChainInvariantInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// checkNetwork compares accepted blocks at the highest height every responding
// node has reached. Faults let nodes lag behind each other, which is fine;
// two nodes with different blocks at the same height is the bug.
func (m *pChainMonitor) checkNetwork(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, pChainInvariantTimeout)
	defer cancel()

	var reachable []pchain.Node
	var minHeight uint64
	// Reserve time for state queries even when a height query stalls.
	heightCtx, cancelHeights := context.WithTimeout(ctx, pChainInvariantTimeout/2)
	heights := getPChainHeights(heightCtx, m.nodes)
	cancelHeights()
	for _, obs := range heights {
		if obs.Err != nil {
			m.log.Debug("skipping unreachable node in invariant check",
				zap.Stringer("node", obs.Node), zap.Error(obs.Err))
			continue
		}
		// A node may stall, but it must not lose previously accepted history.
		if previous, ok := m.nodeHeights[obs.Node.NodeID]; ok {
			assert.Always(obs.Height >= previous, "P-chain accepted height never decreases", map[string]any{
				"node": obs.Node.String(), "previous": previous, "current": obs.Height,
			})
			// Preserve the high-water mark even after a violation.
			if previous > obs.Height {
				continue
			}
		}
		m.nodeHeights[obs.Node.NodeID] = obs.Height
		if len(reachable) == 0 || obs.Height < minHeight {
			minHeight = obs.Height
		}
		reachable = append(reachable, obs.Node)
	}

	// Compare each observation against the first node that answered.
	var blockRef, vdrRef *pChainStateObservation
	states := getPChainStates(ctx, reachable, minHeight)
	for i := range states {
		obs := &states[i]
		if obs.BlockErr == nil {
			if blockRef == nil {
				blockRef = obs
			} else {
				assert.Always(bytes.Equal(blockRef.Block, obs.Block), "P-chain nodes agree on the block at a height", map[string]any{
					"height": minHeight, "reference": blockRef.Node.String(), "node": obs.Node.String(),
					"referenceBlockID": ids.ID(hashing.ComputeHash256Array(blockRef.Block)),
					"blockID":          ids.ID(hashing.ComputeHash256Array(obs.Block)),
				})
			}
		}
		// Detect validator-state reconstruction or cache bugs even when blocks agree.
		if obs.ValidatorsErr == nil {
			if vdrRef == nil {
				vdrRef = obs
			} else {
				assert.Always(reflect.DeepEqual(vdrRef.Validators, obs.Validators), "P-chain nodes agree on the validator set at a height", map[string]any{
					"height": minHeight, "reference": vdrRef.Node.String(), "node": obs.Node.String(),
					"referenceValidators": &platformvm.GetValidatorsAtReply{Validators: vdrRef.Validators},
					"validators":          &platformvm.GetValidatorsAtReply{Validators: obs.Validators},
				})
			}
		}
	}
	m.log.Debug("checked P-chain network state",
		zap.Int("reachable", len(reachable)), zap.Uint64("height", minHeight))
}

func (m *pChainMonitor) recordTx(tx pChainTx) {
	m.pendingLock.Lock()
	defer m.pendingLock.Unlock()
	m.pending = append(m.pending, tx)
}

func (m *pChainMonitor) mergePendingTransactions() {
	m.pendingLock.Lock()
	pending := m.pending
	m.pending = nil
	m.pendingLock.Unlock()
	for i := range pending {
		if len(m.txs) == maxTrackedTxs {
			// The front of the list is the least recently checked transaction.
			m.log.Warn("evicting unresolved P-chain transaction from monitor",
				zap.Stringer("txID", m.txs[0].id))
			m.txs = slices.Delete(m.txs, 0, 1)
		}
		m.txs = append(m.txs, &pending[i])
	}
}

// checkTransactions observes the next batch of unresolved transactions on
// every node. Unresolved transactions rotate to the end of the list so every
// transaction is eventually checked.
func (m *pChainMonitor) checkTransactions(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, pChainInvariantTimeout)
	defer cancel()

	m.mergePendingTransactions()

	count := min(maxCheckedTxs, len(m.txs))
	if count == 0 {
		return
	}
	batch := m.txs[:count]
	observations := make([][]pChainTxObservation, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i, tx := range batch {
		go func() {
			defer wg.Done()
			observations[i] = getPChainTxObservations(ctx, m.nodes, tx.id)
		}()
	}
	wg.Wait()

	// Apply observations on the monitor goroutine, which owns transaction history.
	var (
		unresolved    []*pChainTx
		verifiedCount int
		droppedCount  int
	)
	for i, tx := range batch {
		// Once a transaction is resolved, no further history is retained.
		verified, dropped := m.checkTransaction(tx, observations[i])
		switch {
		case verified:
			verifiedCount++
		case dropped:
			droppedCount++
			m.log.Info("stopped tracking dropped P-chain transaction",
				zap.Stringer("txID", tx.id), zap.String("txType", tx.txType))
		default:
			unresolved = append(unresolved, tx)
		}
	}
	m.txs = slices.Concat(m.txs[count:], unresolved)
	m.log.Info("checked P-chain transactions",
		zap.Int("checked", count),
		zap.Int("verified", verifiedCount),
		zap.Int("dropped", droppedCount),
		zap.Int("tracked", len(m.txs)),
	)
}

// checkTransaction applies one round of observations. It reports verified
// once every node has reported the transaction committed and returned bytes
// that hash to its ID, proving it stored the transaction that was issued. It
// reports dropped once no node has committed the transaction and every node
// has reported it dropped or unknown for droppedChecks consecutive checks.
func (m *pChainMonitor) checkTransaction(tx *pChainTx, observations []pChainTxObservation) (verified, dropped bool) {
	droppedEverywhere := true
	for _, obs := range observations {
		droppedEverywhere = droppedEverywhere && obs.Err == nil &&
			(obs.Status == status.Dropped || obs.Status == status.Unknown)
		// An unavailable response is not evidence that committed state was lost.
		if obs.Err != nil {
			continue
		}
		details := map[string]any{
			"worker": tx.workerID, "txType": tx.txType, "txID": tx.id,
			"issuer": tx.issuer.String(), "node": obs.Node.String(), "status": obs.Status.String(),
		}
		// These user transactions may be dropped, but must never be decided as aborted.
		assert.Always(obs.Status != status.Aborted, "P-chain user transactions never abort", details)
		// Allow lag until this node reports commitment, then require it to retain that decision.
		if tx.committedNodes.Contains(obs.Node.NodeID) {
			assert.Always(obs.Status == status.Committed, "P-chain committed transactions stay committed", details)
		}
		if obs.Status != status.Committed {
			continue
		}
		tx.committedNodes.Add(obs.Node.NodeID)
		// Check that the API returns the transaction identified by the committed ID.
		if obs.BytesErr == nil {
			actualTxID := ids.ID(hashing.ComputeHash256Array(obs.Bytes))
			details["actualTxID"] = actualTxID
			assert.Always(actualTxID == tx.id, "P-chain committed transaction bytes match its ID", details)
			if actualTxID == tx.id {
				tx.verifiedNodes.Add(obs.Node.NodeID)
			}
		}
	}
	// Record successful propagation across observations without imposing a deadline on lagging nodes.
	if tx.committedNodes.Len() == len(m.nodes) {
		assert.Reachable("P-chain transaction committed on every node", map[string]any{
			"worker": tx.workerID, "txID": tx.id, "txType": tx.txType,
		})
	}
	// Status alone is insufficient: every node must also return matching transaction bytes.
	if tx.verifiedNodes.Len() == len(m.nodes) {
		return true, false
	}
	// A transaction committed anywhere stays tracked, so losing it is detected.
	if droppedEverywhere && tx.committedNodes.Len() == 0 {
		tx.droppedStreak++
	} else {
		tx.droppedStreak = 0
	}
	return false, tx.droppedStreak >= droppedChecks
}

type pChainTx struct {
	id             ids.ID
	txType         string
	workerID       int
	issuer         pchain.Node
	committedNodes set.Set[ids.NodeID]
	verifiedNodes  set.Set[ids.NodeID]
	// droppedStreak counts consecutive checks in which every node reported the
	// transaction dropped or unknown.
	droppedStreak int
}

type pChainHeightObservation struct {
	Node   pchain.Node
	Height uint64
	Err    error
}

func getPChainHeights(ctx context.Context, nodes []pchain.Node) []pChainHeightObservation {
	return pchain.ObserveNodes(nodes, func(node pchain.Node) pChainHeightObservation {
		height, err := node.Client.GetHeight(ctx)
		return pChainHeightObservation{Node: node, Height: height, Err: err}
	})
}

type pChainStateObservation struct {
	Node          pchain.Node
	Block         []byte
	BlockErr      error
	Validators    map[ids.NodeID]*validators.GetValidatorOutput
	ValidatorsErr error
}

func getPChainStates(ctx context.Context, nodes []pchain.Node, height uint64) []pChainStateObservation {
	return pchain.ObserveNodes(nodes, func(node pchain.Node) pChainStateObservation {
		obs := pChainStateObservation{Node: node}
		obs.Block, obs.BlockErr = node.Client.GetBlockByHeight(ctx, height)
		obs.Validators, obs.ValidatorsErr = node.Client.GetValidatorsAt(ctx, constants.PrimaryNetworkID, api.Height(height))
		return obs
	})
}

type pChainTxObservation struct {
	pchain.NodeTxStatus
	Bytes    []byte
	BytesErr error
}

func getPChainTxObservations(ctx context.Context, nodes []pchain.Node, txID ids.ID) []pChainTxObservation {
	return pchain.ObserveNodes(nodes, func(node pchain.Node) pChainTxObservation {
		obs := pChainTxObservation{NodeTxStatus: pchain.GetTxStatus(ctx, node, txID)}
		if obs.Status == status.Committed {
			// Fetch bytes before waiting for the other nodes' status requests.
			obs.Bytes, obs.BytesErr = node.Client.GetTx(ctx, txID)
		}
		return obs
	})
}
