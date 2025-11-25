// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
)

type tx ids.ID

func (t tx) GossipID() ids.ID {
	return ids.ID(t)
}

func TestGossiperShutdown(t *testing.T) {
	gossiper := NewPullGossiper[tx](
		logging.NoLog{},
		nil,
		nil,
		nil,
		Metrics{},
		0,
	)
	ctx, cancel := context.WithCancel(t.Context())

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go func() {
		Every(ctx, logging.NoLog{}, gossiper, time.Second)
		wg.Done()
	}()

	cancel()
	wg.Wait()
}

type marshaller struct{}

func (marshaller) MarshalGossip(tx tx) ([]byte, error) {
	return tx[:], nil
}

func (marshaller) UnmarshalGossip(bytes []byte) (tx, error) {
	id, err := ids.ToID(bytes)
	return tx(id), err
}

type setDouble struct {
	txs   set.Set[tx]
	onAdd func(tx tx)
}

func (s *setDouble) Add(t tx) error {
	if s.txs.Contains(t) {
		return fmt.Errorf("%s already present", t)
	}

	s.txs.Add(t)
	if s.onAdd != nil {
		s.onAdd(t)
	}
	return nil
}

func (s *setDouble) Has(h ids.ID) bool {
	return s.txs.Contains(tx(h))
}

func (s *setDouble) Iterate(f func(t tx) bool) {
	for tx := range s.txs {
		if !f(tx) {
			return
		}
	}
}

func (s *setDouble) Len() int {
	return s.txs.Len()
}

func TestGossiperGossip(t *testing.T) {
	tests := []struct {
		name                   string
		targetResponseSize     int
		requester              []tx // what we have
		responder              []tx // what the peer we're requesting gossip from has
		expectedPossibleValues []tx // possible values we can have
		expectedLen            int
		expectedHitRate        float64
	}{
		{
			name: "no gossip - no one knows anything",
		},
		{
			name:                   "no gossip - requester knows more than responder",
			targetResponseSize:     1024,
			requester:              []tx{{0}},
			expectedPossibleValues: []tx{{0}},
			expectedLen:            1,
		},
		{
			name:                   "no gossip - requester knows everything responder knows",
			targetResponseSize:     1024,
			requester:              []tx{{0}},
			responder:              []tx{{0}},
			expectedPossibleValues: []tx{{0}},
			expectedLen:            1,
			expectedHitRate:        100,
		},
		{
			name:                   "gossip - requester knows nothing",
			targetResponseSize:     1024,
			responder:              []tx{{0}},
			expectedPossibleValues: []tx{{0}},
			expectedLen:            1,
			expectedHitRate:        0,
		},
		{
			name:                   "gossip - requester knows less than responder",
			targetResponseSize:     1024,
			requester:              []tx{{0}},
			responder:              []tx{{0}, {1}},
			expectedPossibleValues: []tx{{0}, {1}},
			expectedLen:            2,
			expectedHitRate:        50,
		},
		{
			name:                   "gossip - target response size exceeded",
			targetResponseSize:     32,
			responder:              []tx{{0}, {1}, {2}},
			expectedPossibleValues: []tx{{0}, {1}, {2}},
			expectedLen:            2,
			expectedHitRate:        0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := t.Context()

			responseSender := &enginetest.SenderStub{
				SentAppResponse: make(chan []byte, 1),
			}
			responseNetwork, err := p2p.NewNetwork(
				logging.NoLog{},
				responseSender,
				prometheus.NewRegistry(),
				"",
			)
			require.NoError(err)

			responseSetWithBloom, err := NewSetWithBloomFilter(
				&setDouble{},
				prometheus.NewRegistry(),
				"",
				1000,
				0.01,
				0.05,
			)
			require.NoError(err)
			for _, item := range tt.responder {
				require.NoError(responseSetWithBloom.Add(item))
			}

			metrics, err := NewMetrics(prometheus.NewRegistry(), "")
			require.NoError(err)

			testHistogram := &testHistogram{
				Histogram: metrics.bloomFilterHitRate,
			}
			metrics.bloomFilterHitRate = testHistogram

			marshaller := marshaller{}
			handler := NewHandler[tx](
				logging.NoLog{},
				marshaller,
				responseSetWithBloom,
				metrics,
				tt.targetResponseSize,
			)
			require.NoError(err)
			require.NoError(responseNetwork.AddHandler(0x0, handler))

			requestSender := &enginetest.SenderStub{
				SentAppRequest: make(chan []byte, 1),
			}

			peers := &p2p.Peers{}
			requestNetwork, err := p2p.NewNetwork(
				logging.NoLog{},
				requestSender,
				prometheus.NewRegistry(),
				"",
				peers,
			)
			require.NoError(err)
			require.NoError(requestNetwork.Connected(t.Context(), ids.EmptyNodeID, nil))

			var requestSet setDouble
			requestSetWithBloom, err := NewSetWithBloomFilter(
				&requestSet,
				prometheus.NewRegistry(),
				"",
				1000,
				0.01,
				0.05,
			)
			require.NoError(err)
			for _, item := range tt.requester {
				require.NoError(requestSetWithBloom.Add(item))
			}

			requestClient := requestNetwork.NewClient(
				0x0,
				p2p.PeerSampler{Peers: peers},
			)

			require.NoError(err)
			gossiper := NewPullGossiper[tx](
				logging.NoLog{},
				marshaller,
				requestSetWithBloom,
				requestClient,
				metrics,
				1,
			)
			require.NoError(err)
			received := set.Set[tx]{}
			requestSet.onAdd = func(tx tx) {
				received.Add(tx)
			}

			require.NoError(gossiper.Gossip(ctx))
			require.NoError(responseNetwork.AppRequest(ctx, ids.EmptyNodeID, 1, time.Time{}, <-requestSender.SentAppRequest))
			require.NoError(requestNetwork.AppResponse(ctx, ids.EmptyNodeID, 1, <-responseSender.SentAppResponse))

			require.Len(requestSet.txs, tt.expectedLen)
			require.Subset(tt.expectedPossibleValues, requestSet.txs)
			require.Equal(len(tt.responder) > 0, testHistogram.observed)
			require.Equal(tt.expectedHitRate, testHistogram.observedVal)

			// we should not receive anything that we already had before we
			// requested the gossip
			for _, tx := range tt.requester {
				require.NotContains(received, tx)
			}
		})
	}
}

type gossiperFunc func(ctx context.Context) error

func (f gossiperFunc) Gossip(ctx context.Context) error {
	return f(ctx)
}

func TestEvery(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	gossiper := gossiperFunc(func(context.Context) error {
		if calls >= 10 {
			cancel()
			return nil
		}

		calls++
		return nil
	})

	go Every(ctx, logging.NoLog{}, gossiper, time.Millisecond)
	<-ctx.Done()
}

func TestValidatorGossiper(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()

	validators := &testValidatorSet{
		validators: set.Of(nodeID),
	}

	calls := 0
	gossiper := ValidatorGossiper{
		Gossiper: gossiperFunc(func(context.Context) error {
			calls++
			return nil
		}),
		NodeID:     nodeID,
		Validators: validators,
	}

	// we are a validator, so we should request gossip
	require.NoError(gossiper.Gossip(t.Context()))
	require.Equal(1, calls)

	// we are not a validator, so we should not request gossip
	validators.validators = set.Set[ids.NodeID]{}
	require.NoError(gossiper.Gossip(t.Context()))
	require.Equal(1, calls)
}

func TestPushGossiperNew(t *testing.T) {
	tests := []struct {
		name                 string
		gossipParams         BranchingFactor
		regossipParams       BranchingFactor
		discardedSize        int
		targetGossipSize     int
		maxRegossipFrequency time.Duration
		expected             error
	}{
		{
			name: "invalid gossip num validators",
			gossipParams: BranchingFactor{
				Validators: -1,
			},
			regossipParams: BranchingFactor{
				Peers: 1,
			},
			expected: ErrInvalidNumValidators,
		},
		{
			name: "invalid gossip num non-validators",
			gossipParams: BranchingFactor{
				NonValidators: -1,
			},
			regossipParams: BranchingFactor{
				Peers: 1,
			},
			expected: ErrInvalidNumNonValidators,
		},
		{
			name: "invalid gossip num peers",
			gossipParams: BranchingFactor{
				Peers: -1,
			},
			regossipParams: BranchingFactor{
				Peers: 1,
			},
			expected: ErrInvalidNumPeers,
		},
		{
			name:         "invalid gossip num to gossip",
			gossipParams: BranchingFactor{},
			regossipParams: BranchingFactor{
				Peers: 1,
			},
			expected: ErrInvalidNumToGossip,
		},
		{
			name: "invalid regossip num validators",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				Validators: -1,
			},
			expected: ErrInvalidNumValidators,
		},
		{
			name: "invalid regossip num non-validators",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				NonValidators: -1,
			},
			expected: ErrInvalidNumNonValidators,
		},
		{
			name: "invalid regossip num peers",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				Peers: -1,
			},
			expected: ErrInvalidNumPeers,
		},
		{
			name: "invalid regossip num to gossip",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{},
			expected:       ErrInvalidNumToGossip,
		},
		{
			name: "invalid discarded size",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				Validators: 1,
			},
			discardedSize: -1,
			expected:      ErrInvalidDiscardedSize,
		},
		{
			name: "invalid target gossip size",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				Validators: 1,
			},
			targetGossipSize: -1,
			expected:         ErrInvalidTargetGossipSize,
		},
		{
			name: "invalid max re-gossip frequency",
			gossipParams: BranchingFactor{
				Validators: 1,
			},
			regossipParams: BranchingFactor{
				Validators: 1,
			},
			maxRegossipFrequency: -1,
			expected:             ErrInvalidRegossipFrequency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewPushGossiper[tx](
				nil,
				nil,
				nil,
				nil,
				Metrics{},
				tt.gossipParams,
				tt.regossipParams,
				tt.discardedSize,
				tt.targetGossipSize,
				tt.maxRegossipFrequency,
			)
			require.ErrorIs(t, err, tt.expected)
		})
	}
}

type hasFunc func(id ids.ID) bool

func (h hasFunc) Has(id ids.ID) bool {
	return h(id)
}

// Tests that the outgoing gossip is equivalent to what was accumulated
func TestPushGossiper(t *testing.T) {
	type cycle struct {
		toAdd    []tx
		expected [][]tx
	}
	tests := []struct {
		name           string
		cycles         []cycle
		shouldRegossip bool
	}{
		{
			name: "single cycle with regossip",
			cycles: []cycle{
				{
					toAdd: []tx{
						{0},
						{1},
						{2},
					},
					expected: [][]tx{
						{{0}, {1}, {2}},
					},
				},
			},
			shouldRegossip: true,
		},
		{
			name: "multiple cycles with regossip",
			cycles: []cycle{
				{
					toAdd: []tx{
						{0},
					},
					expected: [][]tx{
						{{0}},
					},
				},
				{
					toAdd: []tx{
						{1},
					},
					expected: [][]tx{
						{{1}},
						{{0}},
					},
				},
				{
					toAdd: []tx{
						{2},
					},
					expected: [][]tx{
						{{2}},
						{{1}, {0}},
					},
				},
			},
			shouldRegossip: true,
		},
		{
			name: "verify that we don't gossip empty messages",
			cycles: []cycle{
				{
					toAdd: []tx{
						{0},
					},
					expected: [][]tx{
						{{0}},
					},
				},
				{
					toAdd:    []tx{},
					expected: [][]tx{},
				},
			},
			shouldRegossip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			ctx := t.Context()

			sender := &enginetest.SenderStub{
				SentAppGossip: make(chan []byte, 2),
			}
			validators := p2p.NewValidators(
				logging.NoLog{},
				constants.PrimaryNetworkID,
				&validatorstest.State{
					GetCurrentHeightF: func(context.Context) (uint64, error) {
						return 1, nil
					},
					GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
						return nil, nil
					},
				},
				time.Hour,
			)
			network, err := p2p.NewNetwork(
				logging.NoLog{},
				sender,
				prometheus.NewRegistry(),
				"",
				validators,
			)
			require.NoError(err)
			client := network.NewClient(0, p2p.PeerSampler{Peers: &p2p.Peers{}})
			metrics, err := NewMetrics(prometheus.NewRegistry(), "")
			require.NoError(err)
			marshaller := marshaller{}

			regossipTime := time.Hour
			if tt.shouldRegossip {
				regossipTime = time.Nanosecond
			}

			gossiper, err := NewPushGossiper[tx](
				marshaller,
				hasFunc(func(ids.ID) bool {
					return true // Never remove the items from the set
				}),
				validators,
				client,
				metrics,
				BranchingFactor{
					Validators: 1,
				},
				BranchingFactor{
					Validators: 1,
				},
				0, // the discarded cache size doesn't matter for this test
				units.MiB,
				regossipTime,
			)
			require.NoError(err)

			for _, cycle := range tt.cycles {
				gossiper.Add(cycle.toAdd...)
				require.NoError(gossiper.Gossip(ctx))

				for _, expected := range cycle.expected {
					want := &sdk.PushGossip{
						Gossip: make([][]byte, 0, len(expected)),
					}

					for _, gossipable := range expected {
						bytes, err := marshaller.MarshalGossip(gossipable)
						require.NoError(err)

						want.Gossip = append(want.Gossip, bytes)
					}

					if len(want.Gossip) > 0 {
						// remove the handler prefix
						sentMsg := <-sender.SentAppGossip
						got := &sdk.PushGossip{}
						require.NoError(proto.Unmarshal(sentMsg[1:], got))

						require.Equal(want.Gossip, got.Gossip)
					} else {
						select {
						case <-sender.SentAppGossip:
							require.FailNow("unexpectedly sent gossip message")
						default:
						}
					}
				}

				if tt.shouldRegossip {
					// Ensure that subsequent calls to `time.Now()` are
					// sufficient for regossip.
					time.Sleep(regossipTime + time.Nanosecond)
				}
			}
		})
	}
}

type testValidatorSet struct {
	validators set.Set[ids.NodeID]
}

func (t testValidatorSet) Len(context.Context) int {
	return len(t.validators)
}

func (t testValidatorSet) Has(_ context.Context, nodeID ids.NodeID) bool {
	return t.validators.Contains(nodeID)
}

type testHistogram struct {
	prometheus.Histogram
	observedVal float64
	observed    bool
}

func (t *testHistogram) Observe(value float64) {
	t.Histogram.Observe(value)
	t.observedVal = value
	t.observed = true
}
