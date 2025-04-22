// Copyright (C) 2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package event

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func newTestSubscription() (*SubscriptionFunc[ids.ID], chan ids.ID) {
	subCh := make(chan ids.ID, 1)
	return &SubscriptionFunc[ids.ID]{
		NotifyF: func(_ context.Context, eventID ids.ID) error {
			subCh <- eventID
			return nil
		},
		Closer: func() error {
			close(subCh)
			return nil
		},
	}, subCh
}

func TestSubscription(t *testing.T) {
	r := require.New(t)
	sub, subCh := newTestSubscription()

	eventID := ids.GenerateTestID()
	r.NoError(sub.Notify(context.Background(), eventID))

	r.Equal(eventID, <-subCh)
	r.NoError(sub.Close())
	<-subCh
}

func TestNotifyAll(t *testing.T) {
	r := require.New(t)
	sub1, sub1Ch := newTestSubscription()
	sub2, sub2Ch := newTestSubscription()

	eventID := ids.GenerateTestID()
	r.NoError(NotifyAll(context.Background(), eventID, sub1, sub2))
	r.Equal(eventID, <-sub1Ch)
	r.Equal(eventID, <-sub2Ch)
}

func TestAggregateSubs(t *testing.T) {
	r := require.New(t)
	sub1, sub1Ch := newTestSubscription()
	sub2, sub2Ch := newTestSubscription()

	eventID := ids.GenerateTestID()
	aggregateSub := Aggregate(sub1, sub2)

	r.NoError(aggregateSub.Notify(context.Background(), eventID))
	r.Equal(eventID, <-sub1Ch)
	r.Equal(eventID, <-sub2Ch)

	r.NoError(aggregateSub.Close())
	<-sub1Ch
	<-sub2Ch
}

func TestMapSub(t *testing.T) {
	r := require.New(t)
	sub, subCh := newTestSubscription()

	mappedSub := Map(func(eventID string) ids.ID { return ids.FromStringOrPanic(eventID) }, sub)
	eventID := ids.GenerateTestID()
	r.NoError(mappedSub.Notify(context.Background(), eventID.String()))
	r.Equal(eventID, <-subCh)
	r.NoError(mappedSub.Close())
	<-subCh
}
