// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package testutil

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/simplex"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// BasicNode is a simple representation of an instance running an epoch. It does not know about any particular custom types, its sole responsibility is to
// run the epoch and handle messages.
type BasicNode struct {
	currentTime atomic.Int64
	shouldStop  atomic.Bool
	lock        sync.RWMutex
	running     sync.WaitGroup
	E           *simplex.Epoch

	ingress chan struct {
		msg  *common.Message
		from common.NodeID
	}
	l                *TestLogger
	t                *testing.T
	messageTypesSent map[string]uint64

	CustomHandler func(msg *common.Message, from common.NodeID) error
}

func NewBasicNode(t *testing.T, epoch *simplex.Epoch, logger *TestLogger) *BasicNode {
	currentTime := epoch.StartTime.UnixMilli()
	bn := BasicNode{
		shouldStop: atomic.Bool{},
		lock:       sync.RWMutex{},
		running:    sync.WaitGroup{},
		E:          epoch,
		ingress: make(chan struct {
			msg  *common.Message
			from common.NodeID
		}, 100000),
		l:                logger,
		t:                t,
		messageTypesSent: make(map[string]uint64),
	}

	bn.currentTime.Store(currentTime)
	return &bn
}

func (b *BasicNode) Start() {
	b.running.Add(1)
	go b.handleMessages()
	require.NoError(b.t, b.E.Start())
}

func (b *BasicNode) AdvanceTime(duration time.Duration) {
	now := time.UnixMilli(b.currentTime.Load()).Add(duration)
	b.currentTime.Store(now.UnixMilli())
	b.E.AdvanceTime(now)
}

func (b *BasicNode) Silence() {
	b.l.Silence()
}

func (b *BasicNode) SilenceExceptKeywords(keywords ...string) {
	b.l.SilenceExceptKeywords(keywords...)
}

func (b *BasicNode) HandleMessage(msg *common.Message, from common.NodeID) error {
	err := b.E.HandleMessage(msg, from)
	if err != nil {
		b.l.Error("error handling message", zap.Stringer("from", from), zap.Stringer("to", b.E.ID), zap.Error(err), zap.Error(err), zap.Any("message", msg))
		panic(fmt.Sprintf("error handling message from %s to %s: %+v. Err message: %v", from.String(), b.E.ID.String(), msg, err))
	}
	require.NoError(b.t, err, "error handling message from %s to %s: %+v", from.String(), b.E.ID.String(), msg)
	return err
}

func (b *BasicNode) handleMessages() {
	defer b.running.Done()
	for msg := range b.ingress {
		if b.shouldStop.Load() {
			return
		}
		var err error
		if b.CustomHandler != nil {
			err = b.CustomHandler(msg.msg, msg.from)
		} else {
			err = b.HandleMessage(msg.msg, msg.from)
		}
		require.NoError(b.t, err)
		if err != nil {
			return
		}
	}
}

func (b *BasicNode) enqueue(msg *common.Message, from, to common.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()
	if b.shouldStop.Load() {
		b.l.Info("Node is stopped, not enqueuing message", zap.Stringer("from", from), zap.Stringer("to", to))
		return
	}

	b.RecordMessageTypeSent(msg)

	if b.shouldStop.Load() {
		b.l.Debug("Node is stopped, not enqueuing message", zap.Stringer("from", from), zap.Stringer("to", to))
		return
	}

	select {
	case b.ingress <- struct {
		msg  *common.Message
		from common.NodeID
	}{msg: msg, from: from}:
	default:
		// drop the message if the ingress channel is full
		formattedString := fmt.Sprintf("Ingress channel is too full, failing test. From %v -> to %v", from, to)
		panic(formattedString)
	}
}

func (b *BasicNode) Stop() {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.E.Stop()
	b.shouldStop.Store(true)
	close(b.ingress)
	b.running.Wait()
}

func (b *BasicNode) RecordMessageTypeSent(msg *common.Message) {
	switch {
	case msg.BlockMessage != nil:
		b.messageTypesSent["BlockMessage"]++
	case msg.VerifiedBlockMessage != nil:
		b.messageTypesSent["VerifiedBlockMessage"]++
	case msg.ReplicationRequest != nil:
		b.messageTypesSent["ReplicationRequest"]++
	case msg.ReplicationResponse != nil:
		b.messageTypesSent["ReplicationResponse"]++
	case msg.Notarization != nil:
		b.messageTypesSent["VerifiedReplicationRequest"]++
	case msg.VerifiedReplicationResponse != nil:
		b.messageTypesSent["VerifiedReplicationResponse"]++
	case msg.Finalization != nil:
		b.messageTypesSent["NotarizationMessage"]++
	case msg.FinalizeVote != nil:
		b.messageTypesSent["FinalizationMessage"]++
	case msg.VoteMessage != nil:
		b.messageTypesSent["VoteMessage"]++
	case msg.EmptyVoteMessage != nil:
		b.messageTypesSent["EmptyVoteMessage"]++
	case msg.EmptyNotarization != nil:
		b.messageTypesSent["EmptyNotarization"]++
	case msg.BlockDigestRequest != nil:
		b.messageTypesSent["BlockDigestRequest"]++
	default:
		panic("unknown message type")
	}
}

func (b *BasicNode) PrintMessageTypesSent() {
	b.t.Logf("Node %s sent message types: %+v", b.E.ID.String(), b.messageTypesSent)
}

func (b *BasicNode) ResetMessageTypesSent() {
	b.messageTypesSent = make(map[string]uint64)
}

func (b *BasicNode) GetMessageTypesSent() map[string]uint64 {
	return b.messageTypesSent
}

func UpdateEpochConfig(epochConfig *simplex.EpochConfig, testConfig *TestNodeConfig) {
	// set the initial storage
	for _, data := range testConfig.InitialStorage {
		err := epochConfig.Storage.Index(context.Background(), data.VerifiedBlock, data.Finalization)
		if err != nil {
			testConfig.Logger.Panic("Failed to update epoch config", zap.Error(err))
		}
	}

	epochConfig.ReplicationEnabled = testConfig.ReplicationEnabled

	// custom communication
	if testConfig.Comm != nil {
		epochConfig.Comm = testConfig.Comm
	}

	if testConfig.SigAggregatorCreator != nil {
		epochConfig.SignatureAggregatorCreator = testConfig.SigAggregatorCreator
	}

	if testConfig.BlockBuilder != nil {
		epochConfig.BlockBuilder = testConfig.BlockBuilder
	}

	if testConfig.MaxRoundWindow != 0 {
		epochConfig.MaxRoundWindow = testConfig.MaxRoundWindow
	}

	if testConfig.MaxBlacklistedRounds != 0 {
		epochConfig.MaxBlacklistedRounds = testConfig.MaxBlacklistedRounds
	}

	if testConfig.Logger != nil {
		epochConfig.Logger = testConfig.Logger
	}

	if testConfig.WAL != nil {
		epochConfig.WAL = testConfig.WAL
	}

	if testConfig.Storage != nil {
		epochConfig.Storage = testConfig.Storage
	}

	if testConfig.StartTime != 0 {
		epochConfig.StartTime = time.UnixMilli(testConfig.StartTime)
	}

	if testConfig.RandomSeed != nil {
		epochConfig.RandomSource = common.NewRandomSourceFromSeed(*testConfig.RandomSeed)
	}
}

// NodeConfig
type TestNodeConfig struct {
	// optional
	InitialStorage       []common.VerifiedFinalizedBlock
	Comm                 common.Communication
	SigAggregatorCreator common.SignatureAggregatorCreator
	ReplicationEnabled   bool
	BlockBuilder         *TestControlledBlockBuilder
	MaxBlacklistedRounds uint16

	// Long Running Tests
	MaxRoundWindow uint64
	Logger         *TestLogger
	WAL            *TestWAL
	Storage        *InMemStorage
	StartTime      int64
	RandomSeed     *int64 // Optional seed for deterministic tests
}
