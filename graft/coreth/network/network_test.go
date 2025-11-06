// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

const (
	codecVersion uint16 = 0
)

var (
	defaultPeerVersion = &version.Application{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	_ message.Request = (*HelloRequest)(nil)
	_                 = (*HelloResponse)(nil)
	_                 = (*GreetingRequest)(nil)
	_                 = (*GreetingResponse)(nil)
	_                 = (*TestMessage)(nil)

	_ message.RequestHandler = (*HelloGreetingRequestHandler)(nil)
	_ message.RequestHandler = (*testRequestHandler)(nil)

	_ common.AppSender = (*testAppSender)(nil)

	_ p2p.Handler = (*testSDKHandler)(nil)
)

func TestNetworkDoesNotConnectToItself(t *testing.T) {
	ctx := snowtest.Context(t, snowtest.CChainID)
	n, err := NewNetwork(ctx, nil, nil, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	require.NoError(t, n.Connected(t.Context(), ctx.NodeID, defaultPeerVersion))
	require.Zero(t, n.Size())
}

func TestRequestAnyRequestsRoutingAndResponse(t *testing.T) {
	callNum := uint32(0)
	senderWg := &sync.WaitGroup{}
	var net Network
	sender := testAppSender{
		sendAppRequestFn: func(_ context.Context, nodes set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			nodeID, _ := nodes.Pop()
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendAppResponseFn: func(nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppResponse(t.Context(), nodeID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(ctx, sender, codecManager, 16, prometheus.NewRegistry())
	require.NoError(t, err)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})
	nodeID := ids.GenerateTestNodeID()
	require.NoError(t, net.Connected(t.Context(), nodeID, defaultPeerVersion))

	requestMessage := HelloRequest{Message: "this is a request"}

	defer net.Shutdown()
	require.NoError(t, net.Connected(t.Context(), nodeID, defaultPeerVersion))

	totalRequests := 5000
	numCallsPerRequest := 1 // on sending response
	totalCalls := totalRequests * numCallsPerRequest

	eg := errgroup.Group{}
	for i := 0; i < totalCalls; i++ {
		eg.Go(func() error {
			requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
			if err != nil {
				return fmt.Errorf("unexpected error during marshal: %w", err)
			}

			responseBytes, _, err := net.SendSyncedAppRequestAny(t.Context(), defaultPeerVersion, requestBytes)
			if err != nil {
				return fmt.Errorf("unexpected error during send: %w", err)
			}

			if responseBytes == nil {
				return errors.New("expected response bytes, got nil")
			}

			var response TestMessage
			if _, err = codecManager.Unmarshal(responseBytes, &response); err != nil {
				return fmt.Errorf("unexpected error during unmarshal: %w", err)
			}
			if response.Message != "Hi" {
				return fmt.Errorf("expected response message 'Hi', got %q", response.Message)
			}
			return nil
		})
	}

	require.NoError(t, eg.Wait())
	senderWg.Wait()
	require.Equal(t, totalCalls, int(atomic.LoadUint32(&callNum)))
}

func TestAppRequestOnCtxCancellation(t *testing.T) {
	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	sender := testAppSender{
		sendAppRequestFn: func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
			return nil
		},
		sendAppResponseFn: func(ids.NodeID, uint32, []byte) error {
			return nil
		},
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(snowCtx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	handler := &HelloGreetingRequestHandler{codec: codecManager}
	net.SetRequestHandler(handler)

	requestMessage := HelloRequest{Message: "this is a request"}
	requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
	require.NoError(t, err)

	nodeID := ids.GenerateTestNodeID()
	ctx, cancel := context.WithCancel(t.Context())
	// cancel context prior to sending
	cancel()
	err = net.SendAppRequest(ctx, nodeID, requestBytes, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestRequestRequestsRoutingAndResponse(t *testing.T) {
	callNum := uint32(0)
	senderWg := &sync.WaitGroup{}
	var net Network
	var lock sync.Mutex
	contactedNodes := make(map[ids.NodeID]struct{})
	sender := testAppSender{
		sendAppRequestFn: func(_ context.Context, nodes set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			nodeID, _ := nodes.Pop()
			lock.Lock()
			contactedNodes[nodeID] = struct{}{}
			lock.Unlock()
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendAppResponseFn: func(nodeID ids.NodeID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppResponse(t.Context(), nodeID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(ctx, sender, codecManager, 16, prometheus.NewRegistry())
	require.NoError(t, err)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})

	nodes := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	for _, nodeID := range nodes {
		require.NoError(t, net.Connected(t.Context(), nodeID, defaultPeerVersion))
	}

	requestMessage := HelloRequest{Message: "this is a request"}
	defer net.Shutdown()

	totalRequests := 5000
	numCallsPerRequest := 1 // on sending response
	totalCalls := totalRequests * numCallsPerRequest

	eg := errgroup.Group{}
	nodeIdx := 0
	for i := 0; i < totalCalls; i++ {
		nodeIdx = (nodeIdx + 1) % (len(nodes))
		eg.Go(func() error {
			requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
			if err != nil {
				return fmt.Errorf("unexpected error during marshal: %w", err)
			}

			responseBytes, _, err := net.SendSyncedAppRequestAny(t.Context(), defaultPeerVersion, requestBytes)
			if err != nil {
				return fmt.Errorf("unexpected error during send: %w", err)
			}

			if responseBytes == nil {
				return errors.New("expected response bytes, got nil")
			}

			var response TestMessage
			if _, err = codecManager.Unmarshal(responseBytes, &response); err != nil {
				return fmt.Errorf("unexpected error during unmarshal: %w", err)
			}
			if response.Message != "Hi" {
				return fmt.Errorf("expected response message 'Hi', got %q", response.Message)
			}
			return nil
		})
	}

	require.NoError(t, eg.Wait())
	senderWg.Wait()
	require.Equal(t, totalCalls, int(atomic.LoadUint32(&callNum)))
	for _, nodeID := range nodes {
		require.Contains(t, contactedNodes, nodeID, "node %s was not contacted", nodeID)
	}

	// ensure empty nodeID is not allowed
	err = net.SendAppRequest(t.Context(), ids.EmptyNodeID, []byte("hello there"), nil)
	require.ErrorIs(t, err, errEmptyNodeID)
}

func TestAppRequestOnShutdown(t *testing.T) {
	var (
		net    Network
		wg     sync.WaitGroup
		called bool
	)
	sender := testAppSender{
		sendAppRequestFn: func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
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
	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(ctx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	nodeID := ids.GenerateTestNodeID()
	require.NoError(t, net.Connected(t.Context(), nodeID, defaultPeerVersion))

	requestMessage := HelloRequest{Message: "this is a request"}
	require.NoError(t, net.Connected(t.Context(), nodeID, defaultPeerVersion))

	errChan := make(chan error, 1)
	go func() {
		requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
		if err != nil {
			errChan <- err
			return
		}
		responseBytes, _, err := net.SendSyncedAppRequestAny(t.Context(), defaultPeerVersion, requestBytes)
		if err != errRequestFailed {
			errChan <- fmt.Errorf("expected errRequestFailed, got %w", err)
			return
		}
		if responseBytes != nil {
			errChan <- fmt.Errorf("expected nil response, got %v", responseBytes)
			return
		}
		errChan <- nil
	}()
	require.NoError(t, <-errChan)
	require.True(t, called)
}

func TestSyncedAppRequestAnyOnCtxCancellation(t *testing.T) {
	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	type reqInfo struct {
		nodeID    ids.NodeID
		requestID uint32
	}
	sentAppRequest := make(chan reqInfo, 1)

	sender := testAppSender{
		sendAppRequestFn: func(ctx context.Context, nodes set.Set[ids.NodeID], requestID uint32, _ []byte) error {
			if err := ctx.Err(); err != nil {
				return err
			}

			require.Len(t, nodes, 1)
			sentAppRequest <- reqInfo{
				nodeID:    nodes.List()[0],
				requestID: requestID,
			}
			return nil
		},
		sendAppResponseFn: func(ids.NodeID, uint32, []byte) error {
			return nil
		},
	}

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(snowCtx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})
	require.NoError(t,
		net.Connected(
			t.Context(),
			ids.GenerateTestNodeID(),
			version.Current,
		),
	)

	requestMessage := HelloRequest{Message: "this is a request"}
	requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
	require.NoError(t, err)

	// cancel context prior to sending
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err = net.SendSyncedAppRequestAny(ctx, defaultPeerVersion, requestBytes)
	require.ErrorIs(t, err, context.Canceled)
	// require we didn't send anything
	select {
	case <-sentAppRequest:
		require.FailNow(t, "should not have sent request")
	default:
	}

	// Cancel context after sending
	require.Empty(t, net.(*network).outstandingRequestHandlers) // no outstanding requests
	ctx, cancel = context.WithCancel(t.Context())
	errChan := make(chan error, 1)
	go func() {
		_, _, err = net.SendSyncedAppRequestAny(ctx, defaultPeerVersion, requestBytes)
		errChan <- err
	}()
	// Wait until we've "sent" the app request over the network
	// before cancelling context.
	sentAppRequestInfo := <-sentAppRequest
	require.Len(t, net.(*network).outstandingRequestHandlers, 1)
	cancel()
	err = <-errChan
	require.ErrorIs(t, err, context.Canceled)
	// Should still be able to process a response after cancelling.
	require.Len(t, net.(*network).outstandingRequestHandlers, 1) // context cancellation SendAppRequestAny failure doesn't clear
	require.NoError(t, net.AppResponse(
		t.Context(),
		sentAppRequestInfo.nodeID,
		sentAppRequestInfo.requestID,
		[]byte{}))
	require.Empty(t, net.(*network).outstandingRequestHandlers) // Received response
}

func TestRequestMinVersion(t *testing.T) {
	callNum := uint32(0)
	nodeID := ids.GenerateTestNodeID()
	codecManager := buildCodec(t, TestMessage{})

	var net Network
	errChan := make(chan error, 1)
	sender := testAppSender{
		sendAppRequestFn: func(_ context.Context, nodes set.Set[ids.NodeID], reqID uint32, _ []byte) error {
			atomic.AddUint32(&callNum, 1)
			require.True(t, nodes.Contains(nodeID), "request nodes should contain expected nodeID")
			require.Len(t, nodes, 1, "request nodes should contain exactly one node")

			go func() {
				time.Sleep(200 * time.Millisecond)
				atomic.AddUint32(&callNum, 1)
				responseBytes, err := codecManager.Marshal(codecVersion, TestMessage{Message: "this is a response"})
				if err != nil {
					errChan <- err
					return
				}
				if err := net.AppResponse(t.Context(), nodeID, reqID, responseBytes); err != nil {
					errChan <- err
					return
				}
			}()
			return nil
		},
	}

	// passing nil as codec works because the net.AppRequest is never called
	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(ctx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	requestMessage := TestMessage{Message: "this is a request"}
	requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
	require.NoError(t, err)
	require.NoError(t,
		net.Connected(
			t.Context(),
			nodeID,
			&version.Application{
				Name:  version.Client,
				Major: 1,
				Minor: 7,
				Patch: 1,
			},
		),
	)

	// ensure version does not match
	responseBytes, _, err := net.SendSyncedAppRequestAny(
		t.Context(),
		&version.Application{
			Name:  version.Client,
			Major: 2,
			Minor: 0,
			Patch: 0,
		},
		requestBytes,
	)
	require.ErrorIs(t, err, errNoPeersFound)
	require.Nil(t, responseBytes)

	// ensure version matches and the request goes through
	responseBytes, _, err = net.SendSyncedAppRequestAny(t.Context(), defaultPeerVersion, requestBytes)
	require.NoError(t, err)

	var response TestMessage
	_, err = codecManager.Unmarshal(responseBytes, &response)
	require.NoError(t, err)
	require.Equal(t, "this is a response", response.Message)

	// Check for errors from the goroutine (non-blocking since SendSyncedAppRequestAny
	// already waited for the response, so the goroutine has completed)
	select {
	case err := <-errChan:
		require.NoError(t, err)
	default:
		// No error, which is expected
	}
}

func TestOnRequestHonoursDeadline(t *testing.T) {
	var net Network
	responded := false
	sender := testAppSender{
		sendAppRequestFn: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []byte) error {
			return nil
		},
		sendAppResponseFn: func(_ ids.NodeID, _ uint32, _ []byte) error {
			responded = true
			return nil
		},
	}

	codecManager := buildCodec(t, TestMessage{})
	requestBytes, err := marshalStruct(codecManager, TestMessage{Message: "hello there"})
	require.NoError(t, err)

	requestHandler := &testRequestHandler{
		processingDuration: 500 * time.Millisecond,
	}

	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err = NewNetwork(ctx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	net.SetRequestHandler(requestHandler)
	nodeID := ids.GenerateTestNodeID()

	requestHandler.response, err = marshalStruct(codecManager, TestMessage{Message: "hi there"})
	require.NoError(t, err)
	require.NoError(t, net.AppRequest(t.Context(), nodeID, 0, time.Now().Add(1*time.Millisecond), requestBytes))
	// ensure the handler didn't get called (as peer.Network would've dropped the request)
	require.Zero(t, requestHandler.calls)

	requestHandler.processingDuration = 0
	require.NoError(t, net.AppRequest(t.Context(), nodeID, 2, time.Now().Add(250*time.Millisecond), requestBytes))
	require.True(t, responded)
	require.Equal(t, uint32(1), requestHandler.calls)
}

func TestHandleInvalidMessages(t *testing.T) {
	codecManager := buildCodec(t, HelloGossip{}, TestMessage{})
	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(1)
	sender := &enginetest.Sender{
		SendAppErrorF: func(context.Context, ids.NodeID, uint32, int32, string) error {
			return nil
		},
	}
	ctx := snowtest.Context(t, snowtest.CChainID)
	clientNetwork, err := NewNetwork(ctx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	clientNetwork.SetRequestHandler(&testRequestHandler{})

	require.NoError(t, clientNetwork.Connected(t.Context(), nodeID, defaultPeerVersion))

	defer clientNetwork.Shutdown()

	// Ensure a valid gossip message sent as any App specific message type does not trigger a fatal error
	marshaller := helloGossipMarshaller{codec: codecManager}
	gossipMsg, err := marshaller.MarshalGossip(&HelloGossip{Msg: "hello there!"})
	require.NoError(t, err)

	// Ensure a valid request message sent as any App specific message type does not trigger a fatal error
	requestMessage, err := marshalStruct(codecManager, TestMessage{Message: "Hello"})
	require.NoError(t, err)

	// Ensure a random message sent as any App specific message type does not trigger a fatal error
	garbageResponse := make([]byte, 10)
	// Ensure a zero-length message sent as any App specific message type does not trigger a fatal error
	emptyResponse := make([]byte, 0)
	// Ensure a nil byte slice sent as any App specific message type does not trigger a fatal error
	var nilResponse []byte

	// Check for edge cases
	require.NoError(t, clientNetwork.AppGossip(t.Context(), nodeID, gossipMsg))
	require.NoError(t, clientNetwork.AppGossip(t.Context(), nodeID, requestMessage))
	require.NoError(t, clientNetwork.AppGossip(t.Context(), nodeID, garbageResponse))
	require.NoError(t, clientNetwork.AppGossip(t.Context(), nodeID, emptyResponse))
	require.NoError(t, clientNetwork.AppGossip(t.Context(), nodeID, nilResponse))
	require.NoError(t, clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), gossipMsg))
	require.NoError(t, clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), requestMessage))
	require.NoError(t, clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), garbageResponse))
	require.NoError(t, clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), emptyResponse))
	require.NoError(t, clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), nilResponse))

	err = clientNetwork.AppResponse(t.Context(), nodeID, requestID, gossipMsg)
	require.ErrorIs(t, err, p2p.ErrUnrequestedResponse)

	err = clientNetwork.AppResponse(t.Context(), nodeID, requestID, requestMessage)
	require.ErrorIs(t, err, p2p.ErrUnrequestedResponse)

	err = clientNetwork.AppResponse(t.Context(), nodeID, requestID, garbageResponse)
	require.ErrorIs(t, err, p2p.ErrUnrequestedResponse)

	err = clientNetwork.AppResponse(t.Context(), nodeID, requestID, emptyResponse)
	require.ErrorIs(t, err, p2p.ErrUnrequestedResponse)

	err = clientNetwork.AppResponse(t.Context(), nodeID, requestID, nilResponse)
	require.ErrorIs(t, err, p2p.ErrUnrequestedResponse)
}

func TestNetworkPropagatesRequestHandlerError(t *testing.T) {
	codecManager := buildCodec(t, TestMessage{})
	nodeID := ids.GenerateTestNodeID()
	requestID := uint32(0)
	sender := testAppSender{}

	ctx := snowtest.Context(t, snowtest.CChainID)
	clientNetwork, err := NewNetwork(ctx, sender, codecManager, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	errTest := errors.New("test error")
	clientNetwork.SetRequestHandler(&testRequestHandler{err: errTest}) // Return an error from the request handler

	require.NoError(t, clientNetwork.Connected(t.Context(), nodeID, defaultPeerVersion))

	defer clientNetwork.Shutdown()

	// Ensure a valid request message sent as any App specific message type does not trigger a fatal error
	requestMessage, err := marshalStruct(codecManager, TestMessage{Message: "Hello"})
	require.NoError(t, err)

	// Check that if the request handler returns an error, it is propagated as a fatal error.
	err = clientNetwork.AppRequest(t.Context(), nodeID, requestID, time.Now().Add(time.Second), requestMessage)
	require.ErrorIs(t, err, errTest)
}

func TestNetworkAppRequestAfterShutdown(t *testing.T) {
	require := require.New(t)

	ctx := snowtest.Context(t, snowtest.CChainID)
	net, err := NewNetwork(ctx, nil, nil, 16, prometheus.NewRegistry())
	require.NoError(err)
	net.Shutdown()

	require.NoError(net.SendAppRequest(t.Context(), ids.GenerateTestNodeID(), nil, nil))
	require.NoError(net.SendAppRequest(t.Context(), ids.GenerateTestNodeID(), nil, nil))
}

func TestNetworkRouting(t *testing.T) {
	require := require.New(t)
	sender := &testAppSender{
		sendAppRequestFn: func(_ context.Context, _ set.Set[ids.NodeID], _ uint32, _ []byte) error {
			return nil
		},
		sendAppResponseFn: func(_ ids.NodeID, _ uint32, _ []byte) error {
			return nil
		},
	}
	protocol := 0
	handler := &testSDKHandler{}

	networkCodec := codec.NewManager(0)
	ctx := snowtest.Context(t, snowtest.CChainID)
	network, err := NewNetwork(ctx, sender, networkCodec, 1, prometheus.NewRegistry())
	require.NoError(err)
	require.NoError(network.AddHandler(uint64(protocol), handler))

	nodeID := ids.GenerateTestNodeID()
	foobar := append([]byte{byte(protocol)}, []byte("foobar")...)
	// forward it to the sdk handler
	require.NoError(network.AppRequest(t.Context(), nodeID, 1, time.Now().Add(5*time.Second), foobar))
	require.True(handler.appRequested)

	err = network.AppResponse(t.Context(), ids.GenerateTestNodeID(), 1, foobar)
	require.ErrorIs(err, p2p.ErrUnrequestedResponse)

	err = network.AppRequestFailed(t.Context(), nodeID, 1, common.ErrTimeout)
	require.ErrorIs(err, p2p.ErrUnrequestedResponse)
}

func buildCodec(t *testing.T, types ...interface{}) codec.Manager {
	codecManager := codec.NewDefaultManager()
	c := linearcodec.NewDefault()
	for _, typ := range types {
		require.NoError(t, c.RegisterType(typ))
	}
	require.NoError(t, codecManager.RegisterCodec(codecVersion, c))
	return codecManager
}

// marshalStruct is a helper method used to marshal an object as `interface{}`
// so that the codec is able to include the TypeID in the resulting bytes
func marshalStruct(codec codec.Manager, obj interface{}) ([]byte, error) {
	return codec.Marshal(codecVersion, &obj)
}

type testAppSender struct {
	sendAppRequestFn  func(context.Context, set.Set[ids.NodeID], uint32, []byte) error
	sendAppResponseFn func(ids.NodeID, uint32, []byte) error
	sendAppGossipFn   func(common.SendConfig, []byte) error
}

func (t testAppSender) SendAppRequest(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, message []byte) error {
	return t.sendAppRequestFn(ctx, nodeIDs, requestID, message)
}

func (t testAppSender) SendAppResponse(_ context.Context, nodeID ids.NodeID, requestID uint32, message []byte) error {
	return t.sendAppResponseFn(nodeID, requestID, message)
}

func (t testAppSender) SendAppGossip(_ context.Context, config common.SendConfig, message []byte) error {
	return t.sendAppGossipFn(config, message)
}

func (testAppSender) SendAppError(context.Context, ids.NodeID, uint32, int32, string) error {
	panic("not implemented")
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

func (h *HelloGreetingRequestHandler) HandleHelloRequest(_ context.Context, _ ids.NodeID, _ uint32, _ *HelloRequest) ([]byte, error) {
	return h.codec.Marshal(codecVersion, HelloResponse{Response: "Hi"})
}

func (h *HelloGreetingRequestHandler) HandleGreetingRequest(_ context.Context, _ ids.NodeID, _ uint32, _ *GreetingRequest) ([]byte, error) {
	return h.codec.Marshal(codecVersion, GreetingResponse{Greet: "Hey there"})
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

func (tx *HelloGossip) GossipID() ids.ID {
	return ids.FromStringOrPanic(tx.Msg)
}

type helloGossipMarshaller struct {
	codec codec.Manager
}

func (g helloGossipMarshaller) MarshalGossip(tx *HelloGossip) ([]byte, error) {
	return g.codec.Marshal(0, tx)
}

func (g helloGossipMarshaller) UnmarshalGossip(bytes []byte) (*HelloGossip, error) {
	h := &HelloGossip{}
	_, err := g.codec.Unmarshal(bytes, h)
	return h, err
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
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.response, r.err
}

type testSDKHandler struct {
	appRequested bool
}

func (*testSDKHandler) AppGossip(context.Context, ids.NodeID, []byte) {
	// TODO implement me
	panic("implement me")
}

func (t *testSDKHandler) AppRequest(context.Context, ids.NodeID, time.Time, []byte) ([]byte, *common.AppError) {
	t.appRequested = true
	return nil, nil
}
