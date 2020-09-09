// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package events

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanche-go/ids"
)

const (
	minBlockerSize = 16
)

// Blocker tracks objects that are blocked
type Blocker map[[32]byte][]Blockable

func (b *Blocker) init() {
	if *b == nil {
		*b = make(map[[32]byte][]Blockable, minBlockerSize)
	}
}

// Fulfill notifies all objects blocking on the event whose ID is <id> that
// the event has happened
func (b *Blocker) Fulfill(id ids.ID) {
	b.init()

	key := id.Key()
	blocking := (*b)[key]
	delete(*b, key)

	for _, pending := range blocking {
		pending.Fulfill(id)
	}
}

// Abandon notifies all objects blocking on the event whose ID is <id> that
// the event has been abandoned
func (b *Blocker) Abandon(id ids.ID) {
	b.init()

	key := id.Key()
	blocking := (*b)[key]
	delete(*b, key)

	for _, pending := range blocking {
		pending.Abandon(id)
	}
}

// Register a new Blockable and its dependencies
func (b *Blocker) Register(pending Blockable) {
	b.init()

	for _, pendingID := range pending.Dependencies().List() {
		key := pendingID.Key()
		(*b)[key] = append((*b)[key], pending)
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
			ids.NewID(key),
			len(value)))
	}

	return strings.TrimSuffix(s.String(), "\n")
}

func (b *Blocker) String() string { return b.PrefixedString("") }
