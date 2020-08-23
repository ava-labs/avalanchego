// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/events"
	"github.com/ava-labs/gecko/utils/wrappers"
)

type common struct {
	// metrics that describe this consensus instance
	metrics

	// context that this consensus instance is executing in
	ctx *snow.Context

	// params describes how this instance was parameterized
	params snowball.Parameters

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
func (c *common) Initialize(ctx *snow.Context, params snowball.Parameters) error {
	c.ctx = ctx
	c.params = params

	if err := c.metrics.Initialize(params.Namespace, params.Metrics); err != nil {
		return fmt.Errorf("failed to initialize metrics: %s", err)
	}
	return params.Valid()
}

// Parameters implements the Snowstorm interface
func (c *common) Parameters() snowball.Parameters { return c.params }

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
