// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import "context"

// Subscriber is an interface that defines a method to wait for events.
// It is used to receive messages from a VM such as Pending transactions, state sync completion, etc.
type Subscriber interface {
	// WaitForEvent blocks until either the given context is cancelled, or a message is returned.
	// It returns the message received, or an error if the context is cancelled.
	WaitForEvent(ctx context.Context) (Message, error)
}

// Subscription is a function that blocks until either the given context is cancelled, or a message is returned.
// It is used to receive messages from a VM such as Pending transactions, state sync completion, etc.
// The function returns the message received, or an error if the context is cancelled.
type Subscription func(ctx context.Context) (Message, error)
