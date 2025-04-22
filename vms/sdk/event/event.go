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

type SubscriptionFunc[T any] struct {
	NotifyF func(ctx context.Context, t T) error
	Closer  func() error
}

func (s SubscriptionFunc[T]) Notify(ctx context.Context, t T) error {
	return s.NotifyF(ctx, t)
}

func (s SubscriptionFunc[_]) Close() error {
	if s.Closer == nil {
		return nil
	}
	return s.Closer()
}

func NotifyAll[T any](ctx context.Context, e T, subs ...Subscription[T]) error {
	var errs []error
	for _, sub := range subs {
		if err := sub.Notify(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

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

func Map[I any, O any](f func(I) O, sub Subscription[O]) Subscription[I] {
	return SubscriptionFunc[I]{
		NotifyF: func(ctx context.Context, t I) error {
			return sub.Notify(ctx, f(t))
		},
		Closer: sub.Close,
	}
}
