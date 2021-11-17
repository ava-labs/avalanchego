// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	minBlockerSize = 16
)

// Blocker tracks objects that are blocked
type Blocker map[ids.ID][]Blockable

func (b *Blocker) init() {
	if *b == nil {
		*b = make(map[ids.ID][]Blockable, minBlockerSize)
	}
}

// Returns the number of items that have dependencies waiting on
// them to be fulfilled
func (b *Blocker) Len() int {
	return len(*b)
}

// Fulfill notifies all objects blocking on the event whose ID is <id> that
// the event has happened
func (b *Blocker) Fulfill(id ids.ID) {
	b.init()

	blocking := (*b)[id]
	delete(*b, id)

	for _, pending := range blocking {
		pending.Fulfill(id)
	}
}

// Abandon notifies all objects blocking on the event whose ID is <id> that
// the event has been abandoned
func (b *Blocker) Abandon(id ids.ID) {
	b.init()

	blocking := (*b)[id]
	delete(*b, id)

	for _, pending := range blocking {
		pending.Abandon(id)
	}
}

// Register a new Blockable and its dependencies
func (b *Blocker) Register(pending Blockable) {
	b.init()

	for pendingID := range pending.Dependencies() {
		(*b)[pendingID] = append((*b)[pendingID], pending)
	}

	pending.Update()
}

// PrefixedString returns the same value as the String function, with all the
// new lines prefixed by [prefix]
func (b *Blocker) PrefixedString(prefix string) string {
	b.init()

	s := strings.Builder{}

	s.WriteString(fmt.Sprintf("Blocking on %d IDs:", len(*b)))

	for key, value := range *b {
		s.WriteString(fmt.Sprintf("\n%sID[%s]: %d",
			prefix,
			key,
			len(value)))
	}

	return strings.TrimSuffix(s.String(), "\n")
}

func (b *Blocker) String() string { return b.PrefixedString("") }
