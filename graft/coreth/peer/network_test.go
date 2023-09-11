// (c) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/coreth/plugin/evm/message"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
)

var (
	defaultPeerVersion = &version.Application{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	_ message.Request = &HelloRequest{}
	_                 = &HelloResponse{}
	_                 = &GreetingRequest{}
	_                 = &GreetingResponse{}
	_                 = &TestMessage{}

	_ message.RequestHandler = &HelloGreetingRequestHandler{}
	_ message.RequestHandler = &testRequestHandler{}

	_ common.AppSender      = testAppSender{}
	_ message.GossipMessage = HelloGossip{}
	_ message.GossipHandler = &testGossipHandler{}

	_ message.CrossChainRequest        = &ExampleCrossChainRequest{}
	_ message.CrossChainRequestHandler = &testCrossChainHandler{}

	_ p2p.Handler = &testSDKHandler{}
)

func TestNetworkDoesNotConnectToItself(t *testing.T) {
	selfNodeID := ids.GenerateTestNodeID()
	n := NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), nil, nil, nil, selfNodeID, 1, 1)
	assert.NoError(t, n.Connected(context.Background(), selfNodeID, defaultPeerVersion))
	assert.EqualValues(t, 0, n.Size())
}

func TestRequestAnyRequestsRoutingAndResponse(t *testing.T) {
	callNum := uint32(0)
	senderWg := &sync.WaitGroup{}
	var net Network
	sender := testAppSender{
		sendAppRequestFn: func(nodes set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			nodeID, _ := nodes.Pop()
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendAppResponseFn: func(nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppResponse(context.Background(), nodeID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 16, 16)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})
	client := NewNetworkClient(net)
	nodeID := ids.GenerateTestNodeID()
	assert.NoError(t, net.Connected(context.Background(), nodeID, defaultPeerVersion))

	requestMessage := HelloRequest{Message: "this is a request"}

	defer net.Shutdown()
	assert.NoError(t, net.Connected(context.Background(), nodeID, defaultPeerVersion))

	totalRequests := 5000
	numCallsPerRequest := 1 // on sending response
	totalCalls := totalRequests * numCallsPerRequest

	requestWg := &sync.WaitGroup{}
	requestWg.Add(totalCalls)
	for i := 0; i < totalCalls; i++ {
		go func(wg *sync.WaitGroup) {
			defer wg.Done()
			requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
			assert.NoError(t, err)
			responseBytes, _, err := client.SendAppRequestAny(defaultPeerVersion, requestBytes)
			assert.NoError(t, err)
			assert.NotNil(t, responseBytes)

			var response TestMessage
			if _, err = codecManager.Unmarshal(responseBytes, &response); err != nil {
				panic(fmt.Errorf("unexpected error during unmarshal: %w", err))
			}
			assert.Equal(t, "Hi", response.Message)
		}(requestWg)
	}

	requestWg.Wait()
	senderWg.Wait()
	assert.Equal(t, totalCalls, int(atomic.LoadUint32(&callNum)))
}

func TestRequestRequestsRoutingAndResponse(t *testing.T) {
	callNum := uint32(0)
	senderWg := &sync.WaitGroup{}
	var net Network
	var lock sync.Mutex
	contactedNodes := make(map[ids.NodeID]struct{})
	sender := testAppSender{
		sendAppRequestFn: func(nodes set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			nodeID, _ := nodes.Pop()
			lock.Lock()
			contactedNodes[nodeID] = struct{}{}
			lock.Unlock()
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendAppResponseFn: func(nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppResponse(context.Background(), nodeID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 16, 16)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})
	client := NewNetworkClient(net)

	nodes := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	for _, nodeID := range nodes {
		assert.NoError(t, net.Connected(context.Background(), nodeID, defaultPeerVersion))
	}

	requestMessage := HelloRequest{Message: "this is a request"}
	defer net.Shutdown()

	totalRequests := 5000
	numCallsPerRequest := 1 // on sending response
	totalCalls := totalRequests * numCallsPerRequest

	requestWg := &sync.WaitGroup{}
	requestWg.Add(totalCalls)
	nodeIdx := 0
	for i := 0; i < totalCalls; i++ {
		nodeIdx = (nodeIdx + 1) % (len(nodes))
		nodeID := nodes[nodeIdx]
		go func(wg *sync.WaitGroup, nodeID ids.NodeID) {
			defer wg.Done()
			requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
			assert.NoError(t, err)
			responseBytes, err := client.SendAppRequest(nodeID, requestBytes)
			assert.NoError(t, err)
			assert.NotNil(t, responseBytes)

			var response TestMessage
			if _, err = codecManager.Unmarshal(responseBytes, &response); err != nil {
				panic(fmt.Errorf("unexpected error during unmarshal: %w", err))
			}
			assert.Equal(t, "Hi", response.Message)
		}(requestWg, nodeID)
	}

	requestWg.Wait()
	senderWg.Wait()
	assert.Equal(t, totalCalls, int(atomic.LoadUint32(&callNum)))
	for _, nodeID := range nodes {
		if _, exists := contactedNodes[nodeID]; !exists {
			t.Fatalf("expected nodeID %s to be contacted but was not", nodeID)
		}
	}

	// ensure empty nodeID is not allowed
	_, err := client.SendAppRequest(ids.EmptyNodeID, []byte("hello there"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot send request to empty nodeID")
}

func TestAppRequestOnShutdown(t *testing.T) {
	var (
		net    Network
		wg     sync.WaitGroup
		called bool
	)
	sender := testAppSender{
		sendAppRequestFn: func(nodes set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			wg.Add(1)
			go func() {
				called = true
				// shutdown the network here to ensure any outstanding requests are handled as failed
				net.Shutdown()
				wg.Done()
			}() // this is on a goroutine to avoid a deadlock since calling Shutdown takes the lock.
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	client := NewNetworkClient(net)
	nodeID := ids.GenerateTestNodeID()
	require.NoError(t, net.Connected(context.Background(), nodeID, defaultPeerVersion))

	requestMessage := HelloRequest{Message: "this is a request"}
	require.NoError(t, net.Connected(context.Background(), nodeID, defaultPeerVersion))

	wg.Add(1)
	go func() {
		defer wg.Done()
		requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
		require.NoError(t, err)
		responseBytes, _, err := client.SendAppRequestAny(defaultPeerVersion, requestBytes)
		require.Error(t, err, ErrRequestFailed)
		require.Nil(t, responseBytes)
	}()
	wg.Wait()
	require.True(t, called)
}

func TestRequestMinVersion(t *testing.T) {
	callNum := uint32(0)
	nodeID := ids.GenerateTestNodeID()
	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	var net Network
	sender := testAppSender{
		sendAppRequestFn: func(nodes set.Set[ids.NodeID], reqID uint32, messageBytes []byte) error {
			atomic.AddUint32(&callNum, 1)
			assert.True(t, nodes.Contains(nodeID), "request nodes should contain expected nodeID")
			assert.Len(t, nodes, 1, "request nodes should contain exactly one node")

			go func() {
				time.Sleep(200 * time.Millisecond)
				atomic.AddUint32(&callNum, 1)
				responseBytes, err := codecManager.Marshal(message.Version, TestMessage{Message: "this is a response"})
				if err != nil {
					panic(err)
				}
				err = net.AppResponse(context.Background(), nodeID, reqID, responseBytes)
				assert.NoError(t, err)
			}()
			return nil
		},
	}

	// passing nil as codec works because the net.AppRequest is never called
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 16)
	client := NewNetworkClient(net)
	requestMessage := TestMessage{Message: "this is a request"}
	requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
	assert.NoError(t, err)
	assert.NoError(t,
		net.Connected(
			context.Background(),
			nodeID,
			&version.Application{
				Major: 1,
				Minor: 7,
				Patch: 1,
			},
		),
	)

	// ensure version does not match
	responseBytes, _, err := client.SendAppRequestAny(
		&version.Application{
			Major: 2,
			Minor: 0,
			Patch: 0,
		},
		requestBytes,
	)
	assert.Equal(t, err.Error(), "no peers found matching version avalanche/2.0.0 out of 1 peers")
	assert.Nil(t, responseBytes)

	// ensure version matches and the request goes through
	responseBytes, _, err = client.SendAppRequestAny(defaultPeerVersion, requestBytes)
	assert.NoError(t, err)

	var response TestMessage
	if _, err = codecManager.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("unexpected error during unmarshal", err)
	}
	assert.Equal(t, "this is a response", response.Message)
}

func TestOnRequestHonoursDeadline(t *testing.T) {
	var net Network
	responded := false
	sender := testAppSender{
		sendAppRequestFn: func(nodes set.Set[ids.NodeID], reqID uint32, message []byte) error {
			return nil
		},
		sendAppResponseFn: func(nodeID ids.NodeID, reqID uint32, message []byte) error {
			responded = true
			return nil
		},
	}

	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	requestBytes, err := marshalStruct(codecManager, TestMessage{Message: "hello there"})
	assert.NoError(t, err)

	requestHandler := &testRequestHandler{
		processingDuration: 500 * time.Millisecond,
	}

	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	net.SetRequestHandler(requestHandler)
	nodeID := ids.GenerateTestNodeID()

	requestHandler.response, err = marshalStruct(codecManager, TestMessage{Message: "hi there"})
	assert.NoError(t, err)
	err = net.AppRequest(context.Background(), nodeID, 1, time.Now().Add(1*time.Millisecond), requestBytes)
	assert.NoError(t, err)
	// ensure the handler didn't get called (as peer.Network would've dropped the request)
	assert.EqualValues(t, requestHandler.calls, 0)

	requestHandler.processingDuration = 0
	err = net.AppRequest(context.Background(), nodeID, 2, time.Now().Add(250*time.Millisecond), requestBytes)
	assert.NoError(t, err)
	assert.True(t, responded)
	assert.EqualValues(t, requestHandler.calls, 1)
}

func TestGossip(t *testing.T) {
	codecManager := buildCodec(t, HelloGossip{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	nodeID := ids.GenerateTestNodeID()
	var clientNetwork Network
	wg := &sync.WaitGroup{}
	sentGossip := false
	wg.Add(1)
	sender := testAppSender{
		sendAppGossipFn: func(msg []byte) error {
			go func() {
				defer wg.Done()
				err := clientNetwork.AppGossip(context.Background(), nodeID, msg)
				assert.NoError(t, err)
			}()
			sentGossip = true
			return nil
		},
	}

	gossipHandler := &testGossipHandler{}
	clientNetwork = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	clientNetwork.SetGossipHandler(gossipHandler)

	assert.NoError(t, clientNetwork.Connected(context.Background(), nodeID, defaultPeerVersion))

	client := NewNetworkClient(clientNetwork)
	defer clientNetwork.Shutdown()

	b, err := buildGossip(codecManager, HelloGossip{Msg: "hello there!"})
	assert.NoError(t, err)

	err = client.Gossip(b)
	assert.NoError(t, err)

	wg.Wait()
	assert.True(t, sentGossip)
	assert.True(t, gossipHandler.received)
}

func TestHandleInvalidMessages(t *testing.T) {
	codecManager := buildCodec(t, HelloGossip{}, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(1)
	sender := testAppSender{}

	clientNetwork := NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	clientNetwork.SetGossipHandler(message.NoopMempoolGossipHandler{})
	clientNetwork.SetRequestHandler(&testRequestHandler{})

	assert.NoError(t, clientNetwork.Connected(context.Background(), nodeID, defaultPeerVersion))

	defer clientNetwork.Shutdown()

	// Ensure a valid gossip message sent as any App specific message type does not trigger a fatal error
	gossipMsg, err := buildGossip(codecManager, HelloGossip{Msg: "hello there!"})
	assert.NoError(t, err)

	// Ensure a valid request message sent as any App specific message type does not trigger a fatal error
	requestMessage, err := marshalStruct(codecManager, TestMessage{Message: "Hello"})
	assert.NoError(t, err)

	// Ensure a random message sent as any App specific message type does not trigger a fatal error
	garbageResponse := make([]byte, 10)
	// Ensure a zero-length message sent as any App specific message type does not trigger a fatal error
	emptyResponse := make([]byte, 0)
	// Ensure a nil byte slice sent as any App specific message type does not trigger a fatal error
	var nilResponse []byte

	// Check for edge cases
	assert.NoError(t, clientNetwork.AppGossip(context.Background(), nodeID, gossipMsg))
	assert.NoError(t, clientNetwork.AppGossip(context.Background(), nodeID, requestMessage))
	assert.NoError(t, clientNetwork.AppGossip(context.Background(), nodeID, garbageResponse))
	assert.NoError(t, clientNetwork.AppGossip(context.Background(), nodeID, emptyResponse))
	assert.NoError(t, clientNetwork.AppGossip(context.Background(), nodeID, nilResponse))
	assert.NoError(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), gossipMsg))
	assert.NoError(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), requestMessage))
	assert.NoError(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), garbageResponse))
	assert.NoError(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), emptyResponse))
	assert.NoError(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), nilResponse))
	assert.ErrorIs(t, p2p.ErrUnrequestedResponse, clientNetwork.AppResponse(context.Background(), nodeID, requestID, gossipMsg))
	assert.ErrorIs(t, p2p.ErrUnrequestedResponse, clientNetwork.AppResponse(context.Background(), nodeID, requestID, requestMessage))
	assert.ErrorIs(t, p2p.ErrUnrequestedResponse, clientNetwork.AppResponse(context.Background(), nodeID, requestID, garbageResponse))
	assert.ErrorIs(t, p2p.ErrUnrequestedResponse, clientNetwork.AppResponse(context.Background(), nodeID, requestID, emptyResponse))
	assert.ErrorIs(t, p2p.ErrUnrequestedResponse, clientNetwork.AppResponse(context.Background(), nodeID, requestID, nilResponse))
}

func TestNetworkPropagatesRequestHandlerError(t *testing.T) {
	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(1)
	sender := testAppSender{}

	clientNetwork := NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	clientNetwork.SetGossipHandler(message.NoopMempoolGossipHandler{})
	clientNetwork.SetRequestHandler(&testRequestHandler{err: errors.New("fail")}) // Return an error from the request handler

	assert.NoError(t, clientNetwork.Connected(context.Background(), nodeID, defaultPeerVersion))

	defer clientNetwork.Shutdown()

	// Ensure a valid request message sent as any App specific message type does not trigger a fatal error
	requestMessage, err := marshalStruct(codecManager, TestMessage{Message: "Hello"})
	assert.NoError(t, err)

	// Check that if the request handler returns an error, it is propagated as a fatal error.
	assert.Error(t, clientNetwork.AppRequest(context.Background(), nodeID, requestID, time.Now().Add(time.Second), requestMessage))
}

func TestCrossChainAppRequest(t *testing.T) {
	var net Network
	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})

	sender := testAppSender{
		sendCrossChainAppRequestFn: func(requestingChainID ids.ID, requestID uint32, requestBytes []byte) error {
			go func() {
				if err := net.CrossChainAppRequest(context.Background(), requestingChainID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendCrossChainAppResponseFn: func(respondingChainID ids.ID, requestID uint32, responseBytes []byte) error {
			go func() {
				if err := net.CrossChainAppResponse(context.Background(), respondingChainID, requestID, responseBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
	}

	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	net.SetCrossChainRequestHandler(&testCrossChainHandler{codec: crossChainCodecManager})
	client := NewNetworkClient(net)

	exampleCrossChainRequest := ExampleCrossChainRequest{
		Message: "hello this is an example request",
	}

	crossChainRequest, err := buildCrossChainRequest(crossChainCodecManager, exampleCrossChainRequest)
	assert.NoError(t, err)

	chainID := ids.ID(ethcommon.BytesToHash([]byte{1, 2, 3, 4, 5}))
	responseBytes, err := client.SendCrossChainRequest(chainID, crossChainRequest)
	assert.NoError(t, err)

	var response ExampleCrossChainResponse
	if _, err = crossChainCodecManager.Unmarshal(responseBytes, &response); err != nil {
		t.Fatal("unexpected error during unmarshal", err)
	}
	assert.Equal(t, "this is an example response", response.Response)
}

func TestCrossChainRequestRequestsRoutingAndResponse(t *testing.T) {
	var (
		callNum  uint32
		senderWg sync.WaitGroup
		net      Network
	)

	sender := testAppSender{
		sendCrossChainAppRequestFn: func(requestingChainID ids.ID, requestID uint32, requestBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.CrossChainAppRequest(context.Background(), requestingChainID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendCrossChainAppResponseFn: func(respondingChainID ids.ID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.CrossChainAppResponse(context.Background(), respondingChainID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	net.SetCrossChainRequestHandler(&testCrossChainHandler{codec: crossChainCodecManager})
	client := NewNetworkClient(net)

	exampleCrossChainRequest := ExampleCrossChainRequest{
		Message: "hello this is an example request",
	}

	chainID := ids.ID(ethcommon.BytesToHash([]byte{1, 2, 3, 4, 5}))
	defer net.Shutdown()

	totalRequests := 500
	numCallsPerRequest := 1 // on sending response
	totalCalls := totalRequests * numCallsPerRequest

	var requestWg sync.WaitGroup
	requestWg.Add(totalCalls)

	for i := 0; i < totalCalls; i++ {
		go func() {
			defer requestWg.Done()
			crossChainRequest, err := buildCrossChainRequest(crossChainCodecManager, exampleCrossChainRequest)
			assert.NoError(t, err)
			responseBytes, err := client.SendCrossChainRequest(chainID, crossChainRequest)
			assert.NoError(t, err)
			assert.NotNil(t, responseBytes)

			var response ExampleCrossChainResponse
			if _, err = crossChainCodecManager.Unmarshal(responseBytes, &response); err != nil {
				panic(fmt.Errorf("unexpected error during unmarshal: %w", err))
			}
			assert.Equal(t, "this is an example response", response.Response)
		}()
	}

	requestWg.Wait()
	senderWg.Wait()
	assert.Equal(t, totalCalls, int(atomic.LoadUint32(&callNum)))
}

func TestCrossChainRequestOnShutdown(t *testing.T) {
	var (
		net    Network
		wg     sync.WaitGroup
		called bool
	)
	sender := testAppSender{
		sendCrossChainAppRequestFn: func(requestingChainID ids.ID, requestID uint32, requestBytes []byte) error {
			wg.Add(1)
			go func() {
				called = true
				// shutdown the network here to ensure any outstanding requests are handled as failed
				net.Shutdown()
				wg.Done()
			}() // this is on a goroutine to avoid a deadlock since calling Shutdown takes the lock.
			return nil
		},
	}
	codecManager := buildCodec(t, TestMessage{})
	crossChainCodecManager := buildCodec(t, ExampleCrossChainRequest{}, ExampleCrossChainResponse{})
	net = NewNetwork(p2p.NewRouter(logging.NoLog{}, nil), sender, codecManager, crossChainCodecManager, ids.EmptyNodeID, 1, 1)
	client := NewNetworkClient(net)

	exampleCrossChainRequest := ExampleCrossChainRequest{
		Message: "hello this is an example request",
	}
	chainID := ids.ID(ethcommon.BytesToHash([]byte{1, 2, 3, 4, 5}))

	wg.Add(1)
	go func() {
		defer wg.Done()
		crossChainRequest, err := buildCrossChainRequest(crossChainCodecManager, exampleCrossChainRequest)
		require.NoError(t, err)
		responseBytes, err := client.SendCrossChainRequest(chainID, crossChainRequest)
		require.ErrorIs(t, err, ErrRequestFailed)
		require.Nil(t, responseBytes)
	}()
	wg.Wait()
	require.True(t, called)
}

func TestNetworkAppRequestAfterShutdown(t *testing.T) {
	require := require.New(t)

	net := NewNetwork(nil, nil, nil, nil, ids.EmptyNodeID, 1, 0)
	net.Shutdown()

	require.NoError(net.SendAppRequest(ids.GenerateTestNodeID(), nil, nil))
	require.NoError(net.SendAppRequest(ids.GenerateTestNodeID(), nil, nil))
}

func TestNetworkCrossChainAppRequestAfterShutdown(t *testing.T) {
	require := require.New(t)

	net := NewNetwork(nil, nil, nil, nil, ids.EmptyNodeID, 0, 1)
	net.Shutdown()

	require.NoError(net.SendCrossChainRequest(ids.GenerateTestID(), nil, nil))
	require.NoError(net.SendCrossChainRequest(ids.GenerateTestID(), nil, nil))
}

func TestSDKRouting(t *testing.T) {
	require := require.New(t)
	sender := &testAppSender{
		sendAppRequestFn: func(s set.Set[ids.NodeID], u uint32, bytes []byte) error {
			return nil
		},
		sendAppResponseFn: func(id ids.NodeID, u uint32, bytes []byte) error {
			return nil
		},
	}
	protocol := 0
	handler := &testSDKHandler{}
	router := p2p.NewRouter(logging.NoLog{}, sender)
	_, err := router.RegisterAppProtocol(uint64(protocol), handler)
	require.NoError(err)

	networkCodec := codec.NewManager(0)
	crossChainCodec := codec.NewManager(0)

	network := NewNetwork(
		router,
		nil,
		networkCodec,
		crossChainCodec,
		ids.EmptyNodeID,
		1,
		1,
	)

	nodeID := ids.GenerateTestNodeID()
	foobar := append([]byte{byte(protocol)}, []byte("foobar")...)
	err = network.AppRequest(context.Background(), nodeID, 0, time.Time{}, foobar)
	require.NoError(err)
	require.True(handler.appRequested)

	err = network.AppResponse(context.Background(), ids.GenerateTestNodeID(), 0, foobar)
	require.ErrorIs(err, p2p.ErrUnrequestedResponse)

	err = network.AppRequestFailed(context.Background(), nodeID, 0)
	require.ErrorIs(err, p2p.ErrUnrequestedResponse)
}

func buildCodec(t *testing.T, types ...interface{}) codec.Manager {
	codecManager := codec.NewDefaultManager()
	c := linearcodec.NewDefault()
	for _, typ := range types {
		assert.NoError(t, c.RegisterType(typ))
	}
	assert.NoError(t, codecManager.RegisterCodec(message.Version, c))
	return codecManager
}

// marshalStruct is a helper method used to marshal an object as `interface{}`
// so that the codec is able to include the TypeID in the resulting bytes
func marshalStruct(codec codec.Manager, obj interface{}) ([]byte, error) {
	return codec.Marshal(message.Version, &obj)
}

func buildGossip(codec codec.Manager, msg message.GossipMessage) ([]byte, error) {
	return codec.Marshal(message.Version, &msg)
}

func buildCrossChainRequest(codec codec.Manager, msg message.CrossChainRequest) ([]byte, error) {
	return codec.Marshal(message.Version, &msg)
}

type testAppSender struct {
	sendCrossChainAppRequestFn  func(ids.ID, uint32, []byte) error
	sendCrossChainAppResponseFn func(ids.ID, uint32, []byte) error
	sendAppRequestFn            func(set.Set[ids.NodeID], uint32, []byte) error
	sendAppResponseFn           func(ids.NodeID, uint32, []byte) error
	sendAppGossipFn             func([]byte) error
}

func (t testAppSender) SendCrossChainAppRequest(_ context.Context, chainID ids.ID, requestID uint32, appRequestBytes []byte) error {
	return t.sendCrossChainAppRequestFn(chainID, requestID, appRequestBytes)
}

func (t testAppSender) SendCrossChainAppResponse(_ context.Context, chainID ids.ID, requestID uint32, appResponseBytes []byte) error {
	return t.sendCrossChainAppResponseFn(chainID, requestID, appResponseBytes)
}

func (t testAppSender) SendAppGossipSpecific(context.Context, set.Set[ids.NodeID], []byte) error {
	panic("not implemented")
}

func (t testAppSender) SendAppRequest(_ context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error {
	return t.sendAppRequestFn(nodeIDs, requestID, message)
}

func (t testAppSender) SendAppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error {
	return t.sendAppResponseFn(nodeID, requestID, message)
}

func (t testAppSender) SendAppGossip(_ context.Context, message []byte) error {
	return t.sendAppGossipFn(message)
}

type HelloRequest struct {
	Message string `serialize:"true"`
}

func (h HelloRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	// casting is only necessary for test since RequestHandler does not implement anything at the moment
	return handler.(TestRequestHandler).HandleHelloRequest(ctx, nodeID, requestID, &h)
}

func (h HelloRequest) String() string {
	return fmt.Sprintf("HelloRequest(%s)", h.Message)
}

type GreetingRequest struct {
	Greeting string `serialize:"true"`
}

func (g GreetingRequest) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	// casting is only necessary for test since RequestHandler does not implement anything at the moment
	return handler.(TestRequestHandler).HandleGreetingRequest(ctx, nodeID, requestID, &g)
}

func (g GreetingRequest) String() string {
	return fmt.Sprintf("GreetingRequest(%s)", g.Greeting)
}

type HelloResponse struct {
	Response string `serialize:"true"`
}

type GreetingResponse struct {
	Greet string `serialize:"true"`
}

type TestRequestHandler interface {
	HandleHelloRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request *HelloRequest) ([]byte, error)
	HandleGreetingRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request *GreetingRequest) ([]byte, error)
}

type HelloGreetingRequestHandler struct {
	message.RequestHandler
	codec codec.Manager
}

func (h *HelloGreetingRequestHandler) HandleHelloRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request *HelloRequest) ([]byte, error) {
	return h.codec.Marshal(message.Version, HelloResponse{Response: "Hi"})
}

func (h *HelloGreetingRequestHandler) HandleGreetingRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, request *GreetingRequest) ([]byte, error) {
	return h.codec.Marshal(message.Version, GreetingResponse{Greet: "Hey there"})
}

type TestMessage struct {
	Message string `serialize:"true"`
}

func (t TestMessage) Handle(ctx context.Context, nodeID ids.NodeID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	return handler.(*testRequestHandler).handleTestRequest(ctx, nodeID, requestID, &t)
}

func (t TestMessage) String() string {
	return fmt.Sprintf("TestMessage(%s)", t.Message)
}

type HelloGossip struct {
	Msg string `serialize:"true"`
}

func (h HelloGossip) Handle(handler message.GossipHandler, nodeID ids.NodeID) error {
	return handler.HandleEthTxs(nodeID, message.EthTxsGossip{})
}

func (h HelloGossip) String() string {
	return fmt.Sprintf("HelloGossip(%s)", h.Msg)
}

func (h HelloGossip) Bytes() []byte {
	// no op
	return nil
}

type testGossipHandler struct {
	received bool
	nodeID   ids.NodeID
}

func (t *testGossipHandler) HandleAtomicTx(nodeID ids.NodeID, msg message.AtomicTxGossip) error {
	t.received = true
	t.nodeID = nodeID
	return nil
}

func (t *testGossipHandler) HandleEthTxs(nodeID ids.NodeID, msg message.EthTxsGossip) error {
	t.received = true
	t.nodeID = nodeID
	return nil
}

type testRequestHandler struct {
	message.RequestHandler
	calls              uint32
	processingDuration time.Duration
	response           []byte
	err                error
}

func (r *testRequestHandler) handleTestRequest(ctx context.Context, _ ids.NodeID, _ uint32, _ *TestMessage) ([]byte, error) {
	r.calls++
	select {
	case <-time.After(r.processingDuration):
		break
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.response, r.err
}

type ExampleCrossChainRequest struct {
	Message string `serialize:"true"`
}

func (e ExampleCrossChainRequest) Handle(ctx context.Context, requestingChainID ids.ID, requestID uint32, handler message.CrossChainRequestHandler) ([]byte, error) {
	return handler.(*testCrossChainHandler).HandleCrossChainRequest(ctx, requestingChainID, requestID, e)
}

func (e ExampleCrossChainRequest) String() string {
	return fmt.Sprintf("TestMessage(%s)", e.Message)
}

type ExampleCrossChainResponse struct {
	Response string `serialize:"true"`
}

type TestCrossChainRequestHandler interface {
	HandleCrossChainRequest(ctx context.Context, requestingchainID ids.ID, requestID uint32, exampleRequest message.CrossChainRequest) ([]byte, error)
}

type testCrossChainHandler struct {
	message.CrossChainRequestHandler
	codec codec.Manager
}

func (t *testCrossChainHandler) HandleCrossChainRequest(ctx context.Context, requestingChainID ids.ID, requestID uint32, exampleRequest message.CrossChainRequest) ([]byte, error) {
	return t.codec.Marshal(message.Version, ExampleCrossChainResponse{Response: "this is an example response"})
}

type testSDKHandler struct {
	appRequested bool
}

func (t *testSDKHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error {
	// TODO implement me
	panic("implement me")
}

func (t *testSDKHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	t.appRequested = true
	return nil, nil
}

func (t *testSDKHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	// TODO implement me
	panic("implement me")
}
