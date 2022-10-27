// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"context"
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
)

const (
	minBlockerSize = 16
)

// Blocker tracks Blockable events.
// Blocker is used to track events that require their dependencies to be
// fulfilled before them. Once a Blockable event is registered, it will be
// notified once any of its dependencies are fulfilled or abandoned.
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
func (b *Blocker) Fulfill(ctx context.Context, id ids.ID) {
	b.init()

	blocking := (*b)[id]
	delete(*b, id)

	for _, pending := range blocking {
		pending.Fulfill(ctx, id)
	}
}

// Abandon notifies all objects blocking on the event whose ID is <id> that
// the event has been abandoned
func (b *Blocker) Abandon(ctx context.Context, id ids.ID) {
	b.init()

	blocking := (*b)[id]
	delete(*b, id)

	for _, pending := range blocking {
		pending.Abandon(ctx, id)
	}
}

// Register a new Blockable and its dependencies
func (b *Blocker) Register(ctx context.Context, pending Blockable) {
	b.init()

	for pendingID := range pending.Dependencies() {
		(*b)[pendingID] = append((*b)[pendingID], pending)
	}

	pending.Update(ctx)
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
