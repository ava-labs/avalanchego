// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCond(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	var (
		noop      = func(*Cond) {}
		signal    = (*Cond).Signal
		broadcast = (*Cond).Broadcast
		merge     = func(ops ...func(*Cond)) func(*Cond) {
			return func(c *Cond) {
				for _, op := range ops {
					op(c)
				}
			}
		}
		tests = []struct {
			name           string
			ctx            context.Context
			expectedErrors []error
			next           []func(*Cond)
		}{
			{
				name:           "signal_once",
				ctx:            t.Context(),
				expectedErrors: make([]error, 1),
				next: []func(*Cond){
					signal,
				},
			},
			{
				name:           "signal_twice",
				ctx:            t.Context(),
				expectedErrors: make([]error, 1),
				next: []func(*Cond){
					merge(
						signal,
						signal,
					),
				},
			},
			{
				name:           "signal_both_once",
				ctx:            t.Context(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					signal,
					signal,
				},
			},
			{
				name:           "signal_both_once_atomically",
				ctx:            t.Context(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					merge(
						signal,
						signal,
					),
					noop,
				},
			},
			{
				name:           "broadcast_once",
				ctx:            t.Context(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					broadcast,
					noop,
				},
			},
			{
				name:           "broadcast_twice",
				ctx:            t.Context(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					broadcast,
					broadcast,
				},
			},
			{
				name: "cancelled",
				ctx:  cancelled,
				expectedErrors: []error{
					context.Canceled,
					context.Canceled,
				},
				next: []func(*Cond){
					noop,
					noop,
				},
			},
		}
		timeout = time.After(time.Second)
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				wg      sync.WaitGroup // WaitGroup to ensure all goroutines have exited before the test ends
				require = require.New(t)
				c       = NewCond(&sync.Mutex{})
				errs    = make(chan error, len(test.expectedErrors))
			)

			wg.Add(len(test.expectedErrors))
			defer wg.Wait()

			for i := range test.expectedErrors {
				// By grabbing the lock outside of the goroutine, we are ensured
				// that all goroutines are waiting after this for loop exits
				// once the lock is no longer held.
				c.L.Lock()
				go func() {
					defer wg.Done()
					t.Log("waiting", i)
					errs <- c.Wait(test.ctx)
					t.Log("waited", i)
					c.L.Unlock()
				}()
			}

			c.L.Lock()
			t.Log("synchronized waiters")
			c.L.Unlock()

			// All goroutines are waiting on the condition variable.

			for i, next := range test.next {
				t.Log("calling next", i)
				next(c)
				t.Log("called next", i)

				select {
				case err := <-errs:
					require.ErrorIs(err, test.expectedErrors[i])
					t.Log("checked error", i)

				// Timing out rather than depending on the test timeout allows
				// the t.Logs to be printed rather than a stack trace.
				case <-timeout:
					t.Log("error checking timeout", i)
					t.Fail()
				}
			}

			// If the test has passed, this lock shouldn't be needed. But it is
			// included to ensure the test doesn't panic if a timeout has fired.
			c.m.Lock()
			defer c.m.Unlock()

			require.Empty(c.w)
		})
	}
}
