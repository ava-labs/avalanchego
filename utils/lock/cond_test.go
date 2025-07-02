// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	log := func(args ...any) {
		t.Log(args...)
	}

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
				ctx:            context.Background(),
				expectedErrors: make([]error, 1),
				next: []func(*Cond){
					signal,
				},
			},
			{
				name:           "signal_twice",
				ctx:            context.Background(),
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
				ctx:            context.Background(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					signal,
					signal,
				},
			},
			{
				name:           "signal_both_once_atomically",
				ctx:            context.Background(),
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
				ctx:            context.Background(),
				expectedErrors: make([]error, 2),
				next: []func(*Cond){
					broadcast,
					noop,
				},
			},
			{
				name:           "broadcast_twice",
				ctx:            context.Background(),
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
				require = require.New(t)
				c       = NewCond(&sync.Mutex{})
				errs    = make(chan error, len(test.expectedErrors))
			)

			for i := range test.expectedErrors {
				// By grabbing the lock outside of the goroutine, we are ensured
				// that all goroutines are waiting after this for loop exits
				// once the lock is no longer held.
				c.L.Lock()
				go func() {
					log("waiting", i)
					errs <- c.Wait(test.ctx)
					log("waited", i)
					c.L.Unlock()
				}()
			}

			c.L.Lock()
			log("synchronized waiters")
			c.L.Unlock()

			// All goroutines are waiting on the condition variable.

			for i, next := range test.next {
				log("calling next", i)
				next(c)
				log("called next", i)

				select {
				case err := <-errs:
					require.ErrorIs(err, test.expectedErrors[i])
					log("checked error", i)

				// Timing out rather than depending on the test timeout allows
				// the logs to be printed rather than a stack trace.
				case <-timeout:
					log("error checking timeout", i)
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
