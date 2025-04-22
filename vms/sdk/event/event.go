// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package event

import (
	"context"
	"errors"
)

var (
	_ Subscription[struct{}]        = (*SubscriptionFunc[struct{}])(nil)
	_ SubscriptionFactory[struct{}] = (*SubscriptionFuncFactory[struct{}])(nil)
)

// SubscriptionFactory returns an instance of a concrete Subscription
type SubscriptionFactory[T any] interface {
	New() (Subscription[T], error)
}

// Subscription defines how to consume events
type Subscription[T any] interface {
	// Notify returns fatal errors
	Notify(ctx context.Context, t T) error
	// Close returns fatal errors
	Close() error
}

type SubscriptionFuncFactory[T any] SubscriptionFunc[T]

func (s SubscriptionFuncFactory[T]) New() (Subscription[T], error) {
	return SubscriptionFunc[T](s), nil
}

// SubscriptionFunc implements Subscription[T] using anonymous functions
type SubscriptionFunc[T any] struct {
	NotifyF func(ctx context.Context, t T) error
	Closer  func() error
}

// Notify invokes the anonymous NotifyF function of the subscription
func (s SubscriptionFunc[T]) Notify(ctx context.Context, t T) error {
	return s.NotifyF(ctx, t)
}

// Close invokes the anonymous Closer function of the subscription if
// non-nil
func (s SubscriptionFunc[_]) Close() error {
	if s.Closer == nil {
		return nil
	}
	return s.Closer()
}

// NotifyAll notifies all subs with the event e and joins any errors returned
// by the combined subs.
func NotifyAll[T any](ctx context.Context, e T, subs ...Subscription[T]) error {
	var errs []error
	for _, sub := range subs {
		if err := sub.Notify(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Aggregate combines the subs into a single subscription instance
func Aggregate[T any](subs ...Subscription[T]) Subscription[T] {
	return SubscriptionFunc[T]{
		NotifyF: func(ctx context.Context, t T) error {
			return NotifyAll(ctx, t, subs...)
		},
		Closer: func() error {
			var errs []error
			for _, sub := range subs {
				if err := sub.Close(); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		},
	}
}

// Map transforms a subscription from an output sub to an input typed sub
func Map[Input any, Output any](mapF func(Input) Output, sub Subscription[Output]) Subscription[Input] {
	return SubscriptionFunc[Input]{
		NotifyF: func(ctx context.Context, t Input) error {
			return sub.Notify(ctx, mapF(t))
		},
		Closer: sub.Close,
	}
}
