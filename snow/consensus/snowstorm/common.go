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
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/wrappers"

	sbcon "github.com/ava-labs/gecko/snow/consensus/snowball"
)

type common struct {
	// metrics that describe this consensus instance
	metrics

	// context that this consensus instance is executing in
	ctx *snow.Context

	// params describes how this instance was parameterized
	params sbcon.Parameters

	// each element of preferences is the ID of a transaction that is preferred
	preferences ids.Set

	// each element of virtuous is the ID of a transaction that is virtuous
	virtuous ids.Set

	// each element is in the virtuous set and is still being voted on
	virtuousVoting ids.Set

	// number of times RecordPoll has been called
	currentVote int

	// keeps track of whether dependencies have been accepted
	pendingAccept events.Blocker

	// keeps track of whether dependencies have been rejected
	pendingReject events.Blocker

	// track any errors that occurred during callbacks
	errs wrappers.Errs
}

// Initialize implements the ConflictGraph interface
func (c *common) Initialize(ctx *snow.Context, params sbcon.Parameters) error {
	c.ctx = ctx
	c.params = params

	if err := c.metrics.Initialize(params.Namespace, params.Metrics); err != nil {
		return fmt.Errorf("failed to initialize metrics: %s", err)
	}
	return params.Valid()
}

// Parameters implements the Snowstorm interface
func (c *common) Parameters() sbcon.Parameters { return c.params }

// Virtuous implements the ConflictGraph interface
func (c *common) Virtuous() ids.Set { return c.virtuous }

// Preferences implements the ConflictGraph interface
func (c *common) Preferences() ids.Set { return c.preferences }

// Quiesce implements the ConflictGraph interface
func (c *common) Quiesce() bool {
	numVirtuous := c.virtuousVoting.Len()
	c.ctx.Log.Verbo("Conflict graph has %d voting virtuous transactions",
		numVirtuous)
	return numVirtuous == 0
}

// Finalized implements the ConflictGraph interface
func (c *common) Finalized() bool {
	numPreferences := c.preferences.Len()
	c.ctx.Log.Verbo("Conflict graph has %d preferred transactions",
		numPreferences)
	return numPreferences == 0
}

// shouldVote returns if the provided tx should be voted on to determine if it
// can be accepted. If the tx can be vacuously accepted, the tx will be accepted
// and will therefore not be valid to be voted on.
func (c *common) shouldVote(con Consensus, tx Tx) (bool, error) {
	if con.Issued(tx) {
		// If the tx was previously inserted, it shouldn't be re-inserted.
		return false, nil
	}

	txID := tx.ID()
	bytes := tx.Bytes()

	// Notify the IPC socket that this tx has been issued.
	c.ctx.DecisionDispatcher.Issue(c.ctx.ChainID, txID, bytes)

	// Notify the metrics that this transaction is being issued.
	c.metrics.Issued(txID)

	// If this tx has inputs, it needs to be voted on before being accepted.
	if inputs := tx.InputIDs(); inputs.Len() != 0 {
		return true, nil
	}

	// Since this tx doesn't have any inputs, it's impossible for there to be
	// any conflicting transactions. Therefore, this transaction is treated as
	// vacuously accepted and doesn't need to be voted on.

	// Accept is called before notifying the IPC so that acceptances that
	// cause fatal errors aren't sent to an IPC peer.
	if err := tx.Accept(); err != nil {
		return false, err
	}

	// Notify the IPC socket that this tx has been accepted.
	c.ctx.DecisionDispatcher.Accept(c.ctx.ChainID, txID, bytes)

	// Notify the metrics that this transaction was just accepted.
	c.metrics.Accepted(txID)
	return false, nil
}

// accept the provided tx.
func (c *common) acceptTx(tx Tx) error {
	// Accept is called before notifying the IPC so that acceptances that cause
	// fatal errors aren't sent to an IPC peer.
	if err := tx.Accept(); err != nil {
		return err
	}

	txID := tx.ID()

	// Notify the IPC socket that this tx has been accepted.
	c.ctx.DecisionDispatcher.Accept(c.ctx.ChainID, txID, tx.Bytes())

	// Update the metrics to account for this transaction's acceptance
	c.metrics.Accepted(txID)

	// If there is a tx that was accepted pending on this tx, the ancestor
	// should be notified that it doesn't need to block on this tx anymore.
	c.pendingAccept.Fulfill(txID)
	// If there is a tx that was issued pending on this tx, the ancestor tx
	// doesn't need to be rejected because of this tx.
	c.pendingReject.Abandon(txID)
	return nil
}

// reject the provided tx.
func (c *common) rejectTx(tx Tx) error {
	// Reject is called before notifying the IPC so that rejections that
	// cause fatal errors aren't sent to an IPC peer.
	if err := tx.Reject(); err != nil {
		return err
	}

	txID := tx.ID()

	// Notify the IPC that the tx was rejected
	c.ctx.DecisionDispatcher.Reject(c.ctx.ChainID, txID, tx.Bytes())

	// Update the metrics to account for this transaction's rejection
	c.metrics.Rejected(txID)

	// If there is a tx that was accepted pending on this tx, the ancestor
	// tx can't be accepted.
	c.pendingAccept.Abandon(txID)
	// If there is a tx that was issued pending on this tx, the ancestor tx
	// must be rejected.
	c.pendingReject.Fulfill(txID)
	return nil
}

// registerAcceptor attempts to accept this tx once all its dependencies are
// accepted. If all the dependencies are already accepted, this function will
// immediately accept the tx.
func (c *common) registerAcceptor(con Consensus, tx Tx) {
	txID := tx.ID()

	toAccept := &acceptor{
		g:    con,
		errs: &c.errs,
		txID: txID,
	}

	for _, dependency := range tx.Dependencies() {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing.
			// This tx should be accepted after this tx is accepted. Note that
			// the dependencies can't already be rejected, because it is assumed
			// that this tx is currently considered valid.
			toAccept.deps.Add(dependency.ID())
		}
	}

	// This tx is no longer being voted on, so we remove it from the voting set.
	// This ensures that virtuous txs built on top of rogue txs don't force the
	// node to treat the rogue tx as virtuous.
	c.virtuousVoting.Remove(txID)
	c.pendingAccept.Register(toAccept)
}

// registerRejector rejects this tx if any of its dependencies are rejected.
func (c *common) registerRejector(con Consensus, tx Tx) {
	// If a tx that this tx depends on is rejected, this tx should also be
	// rejected.
	toReject := &rejector{
		g:    con,
		errs: &c.errs,
		txID: tx.ID(),
	}

	// Register all of this txs dependencies as possibilities to reject this tx.
	for _, dependency := range tx.Dependencies() {
		if dependency.Status() != choices.Accepted {
			// If the dependency isn't accepted, then it must be processing. So,
			// this tx should be rejected if any of these processing txs are
			// rejected. Note that the dependencies can't already be rejected,
			// because it is assumed that this tx is currently considered valid.
			toReject.deps.Add(dependency.ID())
		}
	}

	// Register these dependencies
	c.pendingReject.Register(toReject)
}

// acceptor implements Blockable
type acceptor struct {
	g        Consensus
	errs     *wrappers.Errs
	deps     ids.Set
	rejected bool
	txID     ids.ID
}

func (a *acceptor) Dependencies() ids.Set { return a.deps }

func (a *acceptor) Fulfill(id ids.ID) {
	a.deps.Remove(id)
	a.Update()
}

func (a *acceptor) Abandon(id ids.ID) { a.rejected = true }

func (a *acceptor) Update() {
	// If I was rejected or I am still waiting on dependencies to finish or an
	// error has occurred, I shouldn't do anything.
	if a.rejected || a.deps.Len() != 0 || a.errs.Errored() {
		return
	}
	a.errs.Add(a.g.accept(a.txID))
}

// rejector implements Blockable
type rejector struct {
	g        Consensus
	errs     *wrappers.Errs
	deps     ids.Set
	rejected bool // true if the tx has been rejected
	txID     ids.ID
}

func (r *rejector) Dependencies() ids.Set { return r.deps }

func (r *rejector) Fulfill(ids.ID) {
	if r.rejected || r.errs.Errored() {
		return
	}
	r.rejected = true
	r.errs.Add(r.g.reject(r.txID))
}

func (*rejector) Abandon(ids.ID) {}
func (*rejector) Update()        {}

type snowballNode struct {
	txID               ids.ID
	numSuccessfulPolls int
	confidence         int
}

func (sb *snowballNode) String() string {
	return fmt.Sprintf(
		"SB(NumSuccessfulPolls = %d, Confidence = %d)",
		sb.numSuccessfulPolls,
		sb.confidence)
}

type sortSnowballNodeData []*snowballNode

func (sb sortSnowballNodeData) Less(i, j int) bool {
	return bytes.Compare(sb[i].txID.Bytes(), sb[j].txID.Bytes()) == -1
}
func (sb sortSnowballNodeData) Len() int      { return len(sb) }
func (sb sortSnowballNodeData) Swap(i, j int) { sb[j], sb[i] = sb[i], sb[j] }

func sortSnowballNodes(nodes []*snowballNode) {
	sort.Sort(sortSnowballNodeData(nodes))
}

// ConsensusString converts a list of snowball nodes into a human-readable
// string.
func ConsensusString(name string, nodes []*snowballNode) string {
	// Sort the nodes so that the string representation is canonical
	sortSnowballNodes(nodes)

	sb := strings.Builder{}
	sb.WriteString(name)
	sb.WriteString("(")

	format := fmt.Sprintf(
		"\n    Choice[%s] = ID: %%50s SB(NumSuccessfulPolls = %%d, Confidence = %%d)",
		formatting.IntFormat(len(nodes)-1))
	for i, txNode := range nodes {
		sb.WriteString(fmt.Sprintf(format,
			i, txNode.txID, txNode.numSuccessfulPolls, txNode.confidence))
	}

	if len(nodes) > 0 {
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	return sb.String()
}
