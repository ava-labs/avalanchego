// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
	"strings"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ node = &unaryNode{}
	_ node = &binaryNode{}
)

// TreeFactory implements Factory by returning a tree struct
type TreeFactory struct{}

// New implements Factory
func (TreeFactory) New() Consensus { return &Tree{} }

// Tree implements the snowball interface by using a modified patricia tree.
type Tree struct {
	// node is the root that represents the first snowball instance in the tree,
	// and contains references to all the other snowball instances in the tree.
	node

	// params contains all the configurations of a snowball instance
	params Parameters

	// shouldReset is used as an optimization to prevent needless tree
	// traversals. If a snowball instance does not get an alpha majority, that
	// instance needs to reset by calling RecordUnsuccessfulPoll. Because the
	// tree splits votes based on the branch, when an instance doesn't get an
	// alpha majority none of the children of this instance can get an alpha
	// majority. To avoid calling RecordUnsuccessfulPoll on the full sub-tree of
	// a node that didn't get an alpha majority, shouldReset is used to indicate
	// that any later traversal into this sub-tree should call
	// RecordUnsuccessfulPoll before performing any other action.
	shouldReset bool
}

// Initialize implements the Consensus interface
func (t *Tree) Initialize(params Parameters, choice ids.ID) {
	t.params = params

	snowball := &unarySnowball{}
	snowball.Initialize(params.BetaVirtuous)

	t.node = &unaryNode{
		tree:         t,
		preference:   choice,
		commonPrefix: ids.NumBits, // The initial state has no conflicts
		snowball:     snowball,
	}
}

// Parameters implements the Consensus interface
func (t *Tree) Parameters() Parameters { return t.params }

// Add implements the Consensus interface
func (t *Tree) Add(choice ids.ID) {
	prefix := t.node.DecidedPrefix()
	// Make sure that we haven't already decided against this new id
	if ids.EqualSubset(0, prefix, t.Preference(), choice) {
		t.node = t.node.Add(choice)
	}
}

// RecordPoll implements the Consensus interface
func (t *Tree) RecordPoll(votes ids.Bag) {
	// Get the assumed decided prefix of the root node.
	decidedPrefix := t.node.DecidedPrefix()

	// If any of the bits differ from the preference in this prefix, the vote is
	// for a rejected operation. So, we filter out these invalid votes.
	filteredVotes := votes.Filter(0, decidedPrefix, t.Preference())

	// Now that the votes have been restricted to valid votes, pass them into
	// the first snowball instance
	t.node = t.node.RecordPoll(filteredVotes, t.shouldReset)

	// Because we just passed the reset into the snowball instance, we should no
	// longer reset.
	t.shouldReset = false
}

// RecordUnsuccessfulPoll implements the Consensus interface
func (t *Tree) RecordUnsuccessfulPoll() { t.shouldReset = true }

func (t *Tree) String() string {
	builder := strings.Builder{}

	prefixes := []string{""}
	nodes := []node{t.node}

	for len(prefixes) > 0 {
		newSize := len(prefixes) - 1

		prefix := prefixes[newSize]
		prefixes = prefixes[:newSize]

		node := nodes[newSize]
		nodes = nodes[:newSize]

		s, newNodes := node.Printable()

		builder.WriteString(prefix)
		builder.WriteString(s)
		builder.WriteString("\n")

		newPrefix := prefix + "    "
		for range newNodes {
			prefixes = append(prefixes, newPrefix)
		}
		nodes = append(nodes, newNodes...)
	}

	return strings.TrimSuffix(builder.String(), "\n")
}

type node interface {
	// Preference returns the preferred choice of this sub-tree
	Preference() ids.ID
	// Return the number of assumed decided bits of this node
	DecidedPrefix() int
	// Adds a new choice to vote on
	Add(newChoice ids.ID) node
	// Apply the votes, reset the model if needed
	RecordPoll(votes ids.Bag, shouldReset bool) (newChild node)
	// Returns true if consensus has been reached on this node
	Finalized() bool

	Printable() (string, []node)
}

// unary is a node with either no children, or a single child. It handles the
// voting on a range of identical, virtuous, snowball instances.
type unaryNode struct {
	// tree references the tree that contains this node
	tree *Tree

	// preference is the choice that is preferred at every branch in this
	// sub-tree
	preference ids.ID

	// decidedPrefix is the last bit in the prefix that is assumed to be decided
	decidedPrefix int // Will be in the range [0, 255)

	// commonPrefix is the last bit in the prefix that this node transitively
	// references
	commonPrefix int // Will be in the range (decidedPrefix, 256)

	// snowball wraps the snowball logic
	snowball UnarySnowball

	// shouldReset is used as an optimization to prevent needless tree
	// traversals. It is the continuation of shouldReset in the Tree struct.
	shouldReset bool

	// child is the, possibly nil, node that votes on the next bits in the
	// decision
	child node
}

func (u *unaryNode) Preference() ids.ID { return u.preference }
func (u *unaryNode) DecidedPrefix() int { return u.decidedPrefix }

// This is by far the most complicated function in this algorithm.
// The intuition is that this instance represents a series of consecutive unary
// snowball instances, and this function's purpose is convert one of these unary
// snowball instances into a binary snowball instance.
// There are 5 possible cases.
// 1. None of these instances should be split, we should attempt to split a
//    child
//
//        For example, attempting to insert the value "00001" in this node:
//
//                       +-------------------+ <-- This node will not be split
//                       |                   |
//                       |       0 0 0       |
//                       |                   |
//                       +-------------------+ <-- Pass the add to the child
//                                 ^
//                                 |
//
//        Results in:
//
//                       +-------------------+
//                       |                   |
//                       |       0 0 0       |
//                       |                   |
//                       +-------------------+ <-- With the modified child
//                                 ^
//                                 |
//
// 2. This instance represents a series of only one unary instance and it must
//    be split
//       This will return a binary choice, with one child the same as my child,
//       and another (possibly nil child) representing a new chain to the end of
//       the hash
//
//        For example, attempting to insert the value "1" in this tree:
//
//                       +-------------------+
//                       |                   |
//                       |         0         |
//                       |                   |
//                       +-------------------+
//
//        Results in:
//
//                       +-------------------+
//                       |         |         |
//                       |    0    |    1    |
//                       |         |         |
//                       +-------------------+
//
// 3. This instance must be split on the first bit
//       This will return a binary choice, with one child equal to this instance
//       with decidedPrefix increased by one, and another representing a new
//       chain to the end of the hash
//
//        For example, attempting to insert the value "10" in this tree:
//
//                       +-------------------+
//                       |                   |
//                       |        0 0        |
//                       |                   |
//                       +-------------------+
//
//        Results in:
//
//                       +-------------------+
//                       |         |         |
//                       |    0    |    1    |
//                       |         |         |
//                       +-------------------+
//                            ^         ^
//                           /           \
//            +-------------------+ +-------------------+
//            |                   | |                   |
//            |         0         | |         0         |
//            |                   | |                   |
//            +-------------------+ +-------------------+
//
// 4. This instance must be split on the last bit
//       This will modify this unary choice. The commonPrefix is decreased by
//       one. The child is set to a binary instance that has a child equal to
//       the current child and another child equal to a new unary instance to
//       the end of the hash
//
//        For example, attempting to insert the value "01" in this tree:
//
//                       +-------------------+
//                       |                   |
//                       |        0 0        |
//                       |                   |
//                       +-------------------+
//
//        Results in:
//
//                       +-------------------+
//                       |                   |
//                       |         0         |
//                       |                   |
//                       +-------------------+
//                                 ^
//                                 |
//                       +-------------------+
//                       |         |         |
//                       |    0    |    1    |
//                       |         |         |
//                       +-------------------+
//
// 5. This instance must be split on an interior bit
//       This will modify this unary choice. The commonPrefix is set to the
//       interior bit. The child is set to a binary instance that has a child
//       equal to this unary choice with the decidedPrefix equal to the interior
//       bit and another child equal to a new unary instance to the end of the
//       hash
//
//        For example, attempting to insert the value "010" in this tree:
//
//                       +-------------------+
//                       |                   |
//                       |       0 0 0       |
//                       |                   |
//                       +-------------------+
//
//        Results in:
//
//                       +-------------------+
//                       |                   |
//                       |         0         |
//                       |                   |
//                       +-------------------+
//                                 ^
//                                 |
//                       +-------------------+
//                       |         |         |
//                       |    0    |    1    |
//                       |         |         |
//                       +-------------------+
//                            ^         ^
//                           /           \
//            +-------------------+ +-------------------+
//            |                   | |                   |
//            |         0         | |         0         |
//            |                   | |                   |
//            +-------------------+ +-------------------+
func (u *unaryNode) Add(newChoice ids.ID) node {
	if u.Finalized() {
		return u // Only happens if the tree is finalized, or it's a leaf node
	}

	if index, found := ids.FirstDifferenceSubset(
		u.decidedPrefix, u.commonPrefix, u.preference, newChoice); !found {
		// If the first difference doesn't exist, then this node shouldn't be
		// split
		if u.child != nil {
			// Because this node will finalize before any children could
			// finalize, it must be that the newChoice will match my child's
			// prefix
			u.child = u.child.Add(newChoice)
		}
		// if u.child is nil, then we are attempting to add the same choice into
		// the tree, which should be a noop
	} else {
		// The difference was found, so this node must be split

		bit := u.preference.Bit(uint(index)) // The currently preferred bit
		b := &binaryNode{
			tree:        u.tree,
			bit:         index,
			snowball:    u.snowball.Extend(u.tree.params.BetaRogue, bit),
			shouldReset: [2]bool{u.shouldReset, u.shouldReset},
		}
		b.preferences[bit] = u.preference
		b.preferences[1-bit] = newChoice

		newChildSnowball := &unarySnowball{}
		newChildSnowball.Initialize(u.tree.params.BetaVirtuous)
		newChild := &unaryNode{
			tree:          u.tree,
			preference:    newChoice,
			decidedPrefix: index + 1,   // The new child assumes this branch has decided in it's favor
			commonPrefix:  ids.NumBits, // The new child has no conflicts under this branch
			snowball:      newChildSnowball,
		}

		switch {
		case u.decidedPrefix == u.commonPrefix-1:
			// This node was only voting over one bit. (Case 2. from above)
			b.children[bit] = u.child
			if u.child != nil {
				b.children[1-bit] = newChild
			}
			return b
		case index == u.decidedPrefix:
			// This node was split on the first bit. (Case 3. from above)
			u.decidedPrefix++
			b.children[bit] = u
			b.children[1-bit] = newChild
			return b
		case index == u.commonPrefix-1:
			// This node was split on the last bit. (Case 4. from above)
			u.commonPrefix--
			b.children[bit] = u.child
			if u.child != nil {
				b.children[1-bit] = newChild
			}
			u.child = b
			return u
		default:
			// This node was split on an interior bit. (Case 5. from above)
			originalDecidedPrefix := u.decidedPrefix
			u.decidedPrefix = index + 1
			b.children[bit] = u
			b.children[1-bit] = newChild
			return &unaryNode{
				tree:          u.tree,
				preference:    u.preference,
				decidedPrefix: originalDecidedPrefix,
				commonPrefix:  index,
				snowball:      u.snowball.Clone(),
				child:         b,
			}
		}
	}
	return u // Do nothing, the choice was already rejected
}

func (u *unaryNode) RecordPoll(votes ids.Bag, reset bool) node {
	// We are guaranteed that the votes are of IDs that have previously been
	// added. This ensures that the provided votes all have the same bits in the
	// range [u.decidedPrefix, u.commonPrefix) as in u.preference.

	// If my parent didn't get enough votes previously, then neither did I
	if reset {
		u.snowball.RecordUnsuccessfulPoll()
		u.shouldReset = true // Make sure my child is also reset correctly
	}

	// If I got enough votes this time
	if votes.Len() >= u.tree.params.Alpha {
		u.snowball.RecordSuccessfulPoll()

		if u.child != nil {
			// We are guaranteed that u.commonPrefix will equal
			// u.child.DecidedPrefix(). Otherwise, there must have been a
			// decision under this node, which isn't possible because
			// beta1 <= beta2. That means that filtering the votes between
			// u.commonPrefix and u.child.DecidedPrefix() would always result in
			// the same set being returned.

			// If I'm now decided, return my child
			if u.Finalized() {
				return u.child.RecordPoll(votes, u.shouldReset)
			}
			u.child = u.child.RecordPoll(votes, u.shouldReset)
			// The child's preference may have changed
			u.preference = u.child.Preference()
		}
		// Now that I have passed my votes to my child, I don't need to reset
		// them
		u.shouldReset = false
	} else {
		// I didn't get enough votes, I must reset and my child must reset as
		// well
		u.snowball.RecordUnsuccessfulPoll()
		u.shouldReset = true
	}

	return u
}

func (u *unaryNode) Finalized() bool { return u.snowball.Finalized() }

func (u *unaryNode) Printable() (string, []node) {
	s := fmt.Sprintf("%s Bits = [%d, %d)",
		u.snowball, u.decidedPrefix, u.commonPrefix)
	if u.child == nil {
		return s, nil
	}
	return s, []node{u.child}
}

// binaryNode is a node with either no children, or two children. It handles the
// voting of a single, rogue, snowball instance.
type binaryNode struct {
	// tree references the tree that contains this node
	tree *Tree

	// preferences are the choices that are preferred at every branch in their
	// sub-tree
	preferences [2]ids.ID

	// bit is the index in the id of the choice this node is deciding on
	bit int // Will be in the range [0, 256)

	// snowball wraps the snowball logic
	snowball BinarySnowball

	// shouldReset is used as an optimization to prevent needless tree
	// traversals. It is the continuation of shouldReset in the Tree struct.
	shouldReset [2]bool

	// children are the, possibly nil, nodes that vote on the next bits in the
	// decision
	children [2]node
}

func (b *binaryNode) Preference() ids.ID { return b.preferences[b.snowball.Preference()] }
func (b *binaryNode) DecidedPrefix() int { return b.bit }

func (b *binaryNode) Add(id ids.ID) node {
	bit := id.Bit(uint(b.bit))
	child := b.children[bit]
	// If child is nil, then we are running an instance on the last bit. Finding
	// two hashes that are equal up to the last bit would be really cool though.
	// Regardless, the case is handled
	if child != nil &&
		// + 1 is used because we already explicitly check the p.bit bit
		ids.EqualSubset(b.bit+1, child.DecidedPrefix(), b.preferences[bit], id) {
		b.children[bit] = child.Add(id)
	}
	// If child is nil, then the id has already been added to the tree, so
	// nothing should be done
	// If the decided prefix isn't matched, then a previous decision has made
	// the id that is being added to have already been rejected
	return b
}

func (b *binaryNode) RecordPoll(votes ids.Bag, reset bool) node {
	// The list of votes we are passed is split into votes for bit 0 and votes
	// for bit 1
	splitVotes := votes.Split(uint(b.bit))

	bit := 0
	// We only care about which bit is set if a successful poll can happen
	if splitVotes[1].Len() >= b.tree.params.Alpha {
		bit = 1
	}

	if reset {
		b.snowball.RecordUnsuccessfulPoll()
		b.shouldReset[bit] = true
		// 1-bit isn't set here because it is set below anyway
	}
	b.shouldReset[1-bit] = true // They didn't get the threshold of votes

	prunedVotes := splitVotes[bit]
	// If this bit got alpha votes, it was a successful poll
	if prunedVotes.Len() >= b.tree.params.Alpha {
		b.snowball.RecordSuccessfulPoll(bit)

		if child := b.children[bit]; child != nil {
			// The votes are filtered to ensure that they are votes that should
			// count for the child
			filteredVotes := prunedVotes.Filter(
				b.bit+1, child.DecidedPrefix(), b.preferences[bit])

			if b.snowball.Finalized() {
				// If we are decided here, that means we must have decided due
				// to this poll. Therefore, we must have decided on bit.
				return child.RecordPoll(filteredVotes, b.shouldReset[bit])
			}
			newChild := child.RecordPoll(filteredVotes, b.shouldReset[bit])
			b.children[bit] = newChild
			b.preferences[bit] = newChild.Preference()
		}
		b.shouldReset[bit] = false // We passed the reset down
	} else {
		b.snowball.RecordUnsuccessfulPoll()
		// The winning child didn't get enough votes either
		b.shouldReset[bit] = true
	}
	return b
}

func (b *binaryNode) Finalized() bool { return b.snowball.Finalized() }

func (b *binaryNode) Printable() (string, []node) {
	s := fmt.Sprintf("%s Bit = %d", b.snowball, b.bit)
	if b.children[0] == nil {
		return s, nil
	}
	return s, []node{b.children[1], b.children[0]}
}
