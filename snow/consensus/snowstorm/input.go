// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/formatting"

	sbcon "github.com/ava-labs/gecko/snow/consensus/snowball"
)

// InputFactory implements Factory by returning an input struct
type InputFactory struct{}

// New implements Factory
func (InputFactory) New() Consensus { return &Input{} }

// Input is an implementation of a multi-color, non-transitive, snowball
// instance
type Input struct {
	common

	// Key: Transaction ID
	// Value: Node that represents this transaction in the conflict graph
	txs map[[32]byte]inputTx

	// Key: UTXO ID
	// Value: Node that represents the status of the transactions consuming this
	//        input
	utxos map[[32]byte]inputUtxo
}

type inputTx struct {
	bias int
	tx   Tx

	lastVote int
}

type inputUtxo struct {
	bias, confidence, lastVote int
	rogue                      bool

	preference ids.ID
	color      ids.ID
	conflicts  ids.Set
}

// Initialize implements the ConflictGraph interface
func (ig *Input) Initialize(ctx *snow.Context, params sbcon.Parameters) error {
	ig.txs = make(map[[32]byte]inputTx)
	ig.utxos = make(map[[32]byte]inputUtxo)

	return ig.common.Initialize(ctx, params)
}

// IsVirtuous implements the ConflictGraph interface
func (ig *Input) IsVirtuous(tx Tx) bool {
	id := tx.ID()
	for _, consumption := range tx.InputIDs().List() {
		input := ig.utxos[consumption.Key()]
		if input.rogue ||
			(input.conflicts.Len() > 0 && !input.conflicts.Contains(id)) {
			return false
		}
	}
	return true
}

// Add implements the ConflictGraph interface
func (ig *Input) Add(tx Tx) error {
	if ig.Issued(tx) {
		return nil // Already inserted
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	ig.ctx.DecisionDispatcher.Issue(ig.ctx.ChainID, txID, bytes)
	inputs := tx.InputIDs()
	// If there are no inputs, they are vacuously accepted
	if inputs.Len() == 0 {
		if err := tx.Accept(); err != nil {
			return err
		}
		ig.ctx.DecisionDispatcher.Accept(ig.ctx.ChainID, txID, bytes)
		ig.metrics.Issued(txID)
		ig.metrics.Accepted(txID)
		return nil
	}

	cn := inputTx{tx: tx}
	virtuous := true
	// If there are inputs, they must be voted on
	for _, consumption := range inputs.List() {
		consumptionKey := consumption.Key()
		input, exists := ig.utxos[consumptionKey]
		input.rogue = exists // If the input exists for a conflict
		if exists {
			for _, conflictID := range input.conflicts.List() {
				ig.virtuous.Remove(conflictID)
				ig.virtuousVoting.Remove(conflictID)
			}
		} else {
			input.preference = txID // If there isn't a conflict, I'm preferred
		}
		input.conflicts.Add(txID)
		ig.utxos[consumptionKey] = input

		virtuous = virtuous && !exists
	}

	// Add the node to the set
	ig.txs[txID.Key()] = cn
	if virtuous {
		// If I'm preferred in all my conflict sets, I'm preferred.
		// Because the preference graph is a DAG, there will always be at least
		// one preferred consumer, if there is a consumer
		ig.preferences.Add(txID)
		ig.virtuous.Add(txID)
		ig.virtuousVoting.Add(txID)
	}
	ig.metrics.Issued(txID)

	toReject := &rejector{
		g:    ig,
		errs: &ig.errs,
		txID: txID,
	}

	for _, dependency := range tx.Dependencies() {
		if !dependency.Status().Decided() {
			toReject.deps.Add(dependency.ID())
		}
	}
	ig.pendingReject.Register(toReject)
	return ig.errs.Err
}

// Issued implements the ConflictGraph interface
func (ig *Input) Issued(tx Tx) bool {
	if tx.Status().Decided() {
		return true
	}
	_, ok := ig.txs[tx.ID().Key()]
	return ok
}

// Conflicts implements the ConflictGraph interface
func (ig *Input) Conflicts(tx Tx) ids.Set {
	id := tx.ID()
	conflicts := ids.Set{}

	for _, input := range tx.InputIDs().List() {
		inputNode := ig.utxos[input.Key()]
		conflicts.Union(inputNode.conflicts)
	}

	conflicts.Remove(id)
	return conflicts
}

// RecordPoll implements the ConflictGraph interface
func (ig *Input) RecordPoll(votes ids.Bag) (bool, error) {
	ig.currentVote++
	changed := false

	votes.SetThreshold(ig.params.Alpha)
	threshold := votes.Threshold()
	for _, toInc := range threshold.List() {
		incKey := toInc.Key()
		tx, exist := ig.txs[incKey]
		if !exist {
			// Votes for decided consumptions are ignored
			continue
		}

		tx.bias++

		// The timestamp is needed to ensure correctness in the case that a
		// consumer was rejected from a conflict set, when it was preferred in
		// this conflict set, when there is a tie for the second highest
		// confidence.
		tx.lastVote = ig.currentVote

		preferred := true
		rogue := false
		confidence := ig.params.BetaRogue

		consumptions := tx.tx.InputIDs().List()
		for _, inputID := range consumptions {
			inputKey := inputID.Key()
			input := ig.utxos[inputKey]

			// If I did not receive a vote in the last vote, reset my confidence to 0
			if input.lastVote+1 != ig.currentVote {
				input.confidence = 0
			}
			input.lastVote = ig.currentVote

			// check the snowflake preference
			if !toInc.Equals(input.color) {
				input.confidence = 0
			}
			// update the snowball preference
			if tx.bias > input.bias {
				// if the previous preference lost it's preference in this
				// input, it can't be preferred in all the inputs
				if ig.preferences.Contains(input.preference) {
					ig.preferences.Remove(input.preference)
					changed = true
				}

				input.bias = tx.bias
				input.preference = toInc
			}

			// update snowflake vars
			input.color = toInc
			input.confidence++

			ig.utxos[inputKey] = input

			// track cumulative statistics
			preferred = preferred && toInc.Equals(input.preference)
			rogue = rogue || input.rogue
			if confidence > input.confidence {
				confidence = input.confidence
			}
		}

		// If the node wasn't accepted, but was preferred, make sure it is
		// marked as preferred
		if preferred && !ig.preferences.Contains(toInc) {
			ig.preferences.Add(toInc)
			changed = true
		}

		if (!rogue && confidence >= ig.params.BetaVirtuous) ||
			confidence >= ig.params.BetaRogue {
			ig.deferAcceptance(tx)
			if ig.errs.Errored() {
				return changed, ig.errs.Err
			}
			changed = true
			continue
		}

		ig.txs[incKey] = tx
	}
	return changed, ig.errs.Err
}

func (ig *Input) deferAcceptance(tn inputTx) {
	toAccept := &inputAccepter{
		ig: ig,
		tn: tn,
	}

	for _, dependency := range tn.tx.Dependencies() {
		if !dependency.Status().Decided() {
			toAccept.deps.Add(dependency.ID())
		}
	}

	ig.virtuousVoting.Remove(tn.tx.ID())
	ig.pendingAccept.Register(toAccept)
}

// reject all the ids and remove them from their conflict sets
func (ig *Input) reject(ids ...ids.ID) error {
	for _, conflict := range ids {
		conflictKey := conflict.Key()
		cn := ig.txs[conflictKey]
		delete(ig.txs, conflictKey)
		ig.preferences.Remove(conflict) // A rejected value isn't preferred

		// Remove from all conflict sets
		ig.removeConflict(conflict, cn.tx.InputIDs().List()...)

		// Mark it as rejected
		if err := cn.tx.Reject(); err != nil {
			return err
		}
		ig.ctx.DecisionDispatcher.Reject(ig.ctx.ChainID, cn.tx.ID(), cn.tx.Bytes())
		ig.metrics.Rejected(conflict)
		ig.pendingAccept.Abandon(conflict)
		ig.pendingReject.Fulfill(conflict)
	}
	return nil
}

// Remove id from all of its conflict sets
func (ig *Input) removeConflict(id ids.ID, inputIDs ...ids.ID) {
	for _, inputID := range inputIDs {
		inputKey := inputID.Key()
		// if the input doesn't exists, it was already decided
		if input, exists := ig.utxos[inputKey]; exists {
			input.conflicts.Remove(id)

			// If there is nothing attempting to consume the input, remove it
			// from memory
			if input.conflicts.Len() == 0 {
				delete(ig.utxos, inputKey)
				continue
			}

			// If I was previously preferred, I must find who should now be
			// preferred. This shouldn't normally happen, therefore it is okay
			// to be fairly slow here
			if input.preference.Equals(id) {
				newPreference := ids.ID{}
				newBias := -1
				newBiasTime := 0

				// Find the highest bias conflict
				for _, spend := range input.conflicts.List() {
					tx := ig.txs[spend.Key()]
					if tx.bias > newBias ||
						(tx.bias == newBias &&
							newBiasTime < tx.lastVote) {
						newPreference = spend
						newBias = tx.bias
						newBiasTime = tx.lastVote
					}
				}

				// Set the preferences to the highest bias
				input.preference = newPreference
				input.bias = newBias

				ig.utxos[inputKey] = input

				// We need to check if this node is now preferred
				preferenceNode, exist := ig.txs[newPreference.Key()]
				if exist {
					isPreferred := true
					inputIDs := preferenceNode.tx.InputIDs().List()
					for _, inputID := range inputIDs {
						inputKey := inputID.Key()
						input := ig.utxos[inputKey]

						if !newPreference.Equals(input.preference) {
							// If this preference isn't the preferred color, it
							// isn't preferred. Input might not exist, in which
							// case this still isn't the preferred color
							isPreferred = false
							break
						}
					}
					if isPreferred {
						// If I'm preferred in all my conflict sets, I'm
						// preferred
						ig.preferences.Add(newPreference)
					}
				}
			} else {
				// If i'm rejecting the non-preference, do nothing
				ig.utxos[inputKey] = input
			}
		}
	}
}

func (ig *Input) String() string {
	nodes := []tempNode{}
	for _, tx := range ig.txs {
		id := tx.tx.ID()

		confidence := ig.params.BetaRogue
		for _, inputID := range tx.tx.InputIDs().List() {
			input := ig.utxos[inputID.Key()]
			if input.lastVote != ig.currentVote {
				confidence = 0
				break
			}
			if !id.Equals(input.color) {
				confidence = 0
				break
			}

			if input.confidence < confidence {
				confidence = input.confidence
			}
		}

		nodes = append(nodes, tempNode{
			id:         id,
			bias:       tx.bias,
			confidence: confidence,
		})
	}
	sortTempNodes(nodes)

	sb := strings.Builder{}

	sb.WriteString("IG(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s %%s",
		formatting.IntFormat(len(nodes)-1))

	for i, cn := range nodes {
		sb.WriteString(fmt.Sprintf(format, i, cn.id, &cn))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")

	return sb.String()
}

type inputAccepter struct {
	ig       *Input
	deps     ids.Set
	rejected bool
	tn       inputTx
}

func (a *inputAccepter) Dependencies() ids.Set { return a.deps }

func (a *inputAccepter) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *inputAccepter) Abandon(id ids.ID) { a.rejected = true }

func (a *inputAccepter) Update() {
	if a.rejected || a.deps.Len() != 0 || a.ig.errs.Errored() {
		return
	}

	id := a.tn.tx.ID()
	delete(a.ig.txs, id.Key())

	// Remove Tx from all of its conflicts
	inputIDs := a.tn.tx.InputIDs()
	a.ig.removeConflict(id, inputIDs.List()...)

	a.ig.virtuous.Remove(id)
	a.ig.preferences.Remove(id)

	// Reject the conflicts
	conflicts := ids.Set{}
	for inputKey, exists := range inputIDs {
		if exists {
			inputNode := a.ig.utxos[inputKey]
			conflicts.Union(inputNode.conflicts)
		}
	}
	if err := a.ig.reject(conflicts.List()...); err != nil {
		a.ig.errs.Add(err)
		return
	}

	// Mark it as accepted
	if err := a.tn.tx.Accept(); err != nil {
		a.ig.errs.Add(err)
		return
	}
	a.ig.ctx.DecisionDispatcher.Accept(a.ig.ctx.ChainID, id, a.tn.tx.Bytes())
	a.ig.metrics.Accepted(id)

	a.ig.pendingAccept.Fulfill(id)
	a.ig.pendingReject.Abandon(id)
}

type tempNode struct {
	id               ids.ID
	bias, confidence int
}

func (tn *tempNode) String() string {
	return fmt.Sprintf(
		"SB(NumSuccessfulPolls = %d, Confidence = %d)",
		tn.bias,
		tn.confidence)
}

type sortTempNodeData []tempNode

func (tnd sortTempNodeData) Less(i, j int) bool {
	return bytes.Compare(tnd[i].id.Bytes(), tnd[j].id.Bytes()) == -1
}
func (tnd sortTempNodeData) Len() int      { return len(tnd) }
func (tnd sortTempNodeData) Swap(i, j int) { tnd[j], tnd[i] = tnd[i], tnd[j] }

func sortTempNodes(nodes []tempNode) { sort.Sort(sortTempNodeData(nodes)) }
