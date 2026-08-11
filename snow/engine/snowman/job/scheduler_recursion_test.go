// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package job

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// abandonChainJob mirrors the block issuer: when its parent dependency is
// abandoned, it abandons its own dependency, which unblocks the next job in a
// linear chain of dependent blocks.
type abandonChainJob struct {
	scheduler  *Scheduler[int]
	dependency int
	depth      *int
	maxDepth   *int
}

func (j *abandonChainJob) Execute(ctx context.Context, _ []int, abandoned []int) error {
	*j.depth++
	if *j.depth > *j.maxDepth {
		*j.maxDepth = *j.depth
	}
	defer func() {
		*j.depth--
	}()

	if len(abandoned) == 0 {
		return nil
	}
	return j.scheduler.Abandon(ctx, j.dependency)
}

// A chain of N blocks that all get abandoned must not nest Execute N calls deep.
// The scheduler drains the chain iteratively, so the stack stays flat and a long
// backlog cannot overflow it.
func TestScheduler_AbandonLongChainDoesNotRecurse(t *testing.T) {
	require := require.New(t)

	const (
		chainLen       = 50_000
		maxNestedDepth = 16
	)

	s := NewScheduler[int]()
	ctx := t.Context()
	depth, maxDepth := 0, 0
	for k := 1; k <= chainLen; k++ {
		job := &abandonChainJob{
			scheduler:  s,
			dependency: k,
			depth:      &depth,
			maxDepth:   &maxDepth,
		}
		require.NoError(s.Schedule(ctx, job, k-1))
	}

	require.NoError(s.Abandon(ctx, 0))

	require.Less(
		maxDepth,
		maxNestedDepth,
		"abandoning a %d-block chain nested Execute %d levels deep; a chain this long overflows the stack",
		chainLen,
		maxDepth,
	)
}
