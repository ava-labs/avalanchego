// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"crypto/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

var defaultMaxTasks uint64 = 1000

func TestAsyncScheduler(t *testing.T) {
	t.Run("Executes asynchronously", func(t *testing.T) {
		as := NewScheduler(noopLogger{}, defaultMaxTasks)
		defer as.Close()

		ticks := make(chan struct{})

		var wg sync.WaitGroup
		wg.Add(1)

		dig2 := makeDigest(t)

		as.Schedule(func() Digest {
			defer wg.Done()
			<-ticks
			return dig2
		})

		ticks <- struct{}{}
		wg.Wait()
	})

	t.Run("Does not execute when closed", func(t *testing.T) {
		as := NewScheduler(noopLogger{}, defaultMaxTasks)
		ticks := make(chan struct{}, 1)

		as.Close()

		dig := makeDigest(t)
		as.Schedule(func() Digest {
			close(ticks)
			return dig
		})

		ticks <- struct{}{}
	})
}

func makeDigest(t *testing.T) Digest {
	var dig Digest
	_, err := rand.Read(dig[:])
	require.NoError(t, err)
	return dig
}
