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

	"github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/subnet-evm/plugin/evm/message"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/version"
	"github.com/stretchr/testify/assert"
)

var (
	defaultPeerVersion = version.NewDefaultApplication("subnet-evmtest", 1, 0, 0)

	_ message.Request = &HelloRequest{}
	_                 = &HelloResponse{}
	_                 = &GreetingRequest{}
	_                 = &GreetingResponse{}
	_                 = &TestMessage{}

	_ message.RequestHandler = &HelloGreetingRequestHandler{}
	_ message.RequestHandler = &testRequestHandler{}

	_ common.AppSender      = testAppSender{}
	_ message.Message       = HelloGossip{}
	_ message.GossipHandler = &testGossipHandler{}
)

func TestNetworkDoesNotConnectToItself(t *testing.T) {
	selfNodeID := ids.GenerateTestShortID()
	n := NewNetwork(nil, nil, selfNodeID, 1)
	assert.NoError(t, n.Connected(selfNodeID, version.NewDefaultApplication("avalanchego", 1, 0, 0)))
	assert.EqualValues(t, 0, n.Size())
}

func TestRequestsRoutingAndResponse(t *testing.T) {
	callNum := uint32(0)
	senderWg := &sync.WaitGroup{}
	var net Network
	sender := testAppSender{
		sendAppRequestFn: func(nodes ids.ShortSet, requestID uint32, requestBytes []byte) error {
			nodeID, _ := nodes.Pop()
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppRequest(nodeID, requestID, time.Now().Add(5*time.Second), requestBytes); err != nil {
					panic(err)
				}
			}()
			return nil
		},
		sendAppResponseFn: func(nodeID ids.ShortID, requestID uint32, responseBytes []byte) error {
			senderWg.Add(1)
			go func() {
				defer senderWg.Done()
				if err := net.AppResponse(nodeID, requestID, responseBytes); err != nil {
					panic(err)
				}
				atomic.AddUint32(&callNum, 1)
			}()
			return nil
		},
	}

	codecManager := buildCodec(t, HelloRequest{}, HelloResponse{})
	net = NewNetwork(sender, codecManager, ids.ShortEmpty, 16)
	net.SetRequestHandler(&HelloGreetingRequestHandler{codec: codecManager})
	client := NewClient(net)
	nodeID := ids.GenerateTestShortID()
	assert.NoError(t, net.Connected(nodeID, defaultPeerVersion))

	requestMessage := HelloRequest{Message: "this is a request"}

	defer net.Shutdown()
	assert.NoError(t, net.Connected(nodeID, defaultPeerVersion))

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
			responseBytes, failed, err := client.RequestAny(defaultPeerVersion, requestBytes)
			assert.NoError(t, err)
			assert.False(t, failed)
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

func TestRequestMinVersion(t *testing.T) {
	callNum := uint32(0)
	nodeID := ids.GenerateTestShortID()
	codecManager := buildCodec(t, TestMessage{})

	var net Network
	sender := testAppSender{
		sendAppRequestFn: func(nodes ids.ShortSet, reqID uint32, messageBytes []byte) error {
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
				err = net.AppResponse(nodeID, reqID, responseBytes)
				assert.NoError(t, err)
			}()
			return nil
		},
	}

	// passing nil as codec works because the net.AppRequest is never called
	net = NewNetwork(sender, codecManager, ids.ShortEmpty, 1)
	client := NewClient(net)
	requestMessage := TestMessage{Message: "this is a request"}
	requestBytes, err := message.RequestToBytes(codecManager, requestMessage)
	assert.NoError(t, err)
	assert.NoError(t, net.Connected(nodeID, version.NewDefaultApplication("subnet-evmtest", 1, 7, 1)))

	// ensure version does not match
	responseBytes, failed, err := client.RequestAny(version.NewDefaultApplication("subnet-evmtest", 2, 0, 0), requestBytes)
	assert.Equal(t, err.Error(), "no peers found matching version subnet-evmtest/2.0.0 out of 1 peers")
	assert.True(t, failed)
	assert.Nil(t, responseBytes)

	// ensure version matches and the request goes through
	responseBytes, failed, err = client.RequestAny(version.NewDefaultApplication("subnet-evmtest", 1, 0, 0), requestBytes)
	assert.NoError(t, err)
	assert.False(t, failed)

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
		sendAppRequestFn: func(nodes ids.ShortSet, reqID uint32, message []byte) error {
			return nil
		},
		sendAppResponseFn: func(nodeID ids.ShortID, reqID uint32, message []byte) error {
			responded = true
			return nil
		},
	}

	codecManager := buildCodec(t, TestMessage{})

	requestBytes, err := marshalStruct(codecManager, TestMessage{Message: "hello there"})
	assert.NoError(t, err)

	requestHandler := &testRequestHandler{
		processingDuration: 500 * time.Millisecond,
	}
	net = NewNetwork(sender, codecManager, ids.ShortEmpty, 1)
	net.SetRequestHandler(requestHandler)
	nodeID := ids.GenerateTestShortID()

	requestHandler.response, err = marshalStruct(codecManager, TestMessage{Message: "hi there"})
	assert.NoError(t, err)
	err = net.AppRequest(nodeID, 1, time.Now().Add(1*time.Millisecond), requestBytes)
	assert.NoError(t, err)
	// ensure the handler didn't get called (as peer.Network would've dropped the request)
	assert.EqualValues(t, requestHandler.calls, 0)

	requestHandler.processingDuration = 0
	err = net.AppRequest(nodeID, 2, time.Now().Add(250*time.Millisecond), requestBytes)
	assert.NoError(t, err)
	assert.True(t, responded)
	assert.EqualValues(t, requestHandler.calls, 1)
}

func TestGossip(t *testing.T) {
	codecManager := buildCodec(t, HelloGossip{})

	nodeID := ids.GenerateTestShortID()
	var clientNetwork Network
	wg := &sync.WaitGroup{}
	sentGossip := false
	wg.Add(1)
	sender := testAppSender{
		sendAppGossipFn: func(msg []byte) error {
			go func() {
				defer wg.Done()
				err := clientNetwork.AppGossip(nodeID, msg)
				assert.NoError(t, err)
			}()
			sentGossip = true
			return nil
		},
	}

	gossipHandler := &testGossipHandler{}
	clientNetwork = NewNetwork(sender, codecManager, ids.ShortEmpty, 1)
	clientNetwork.SetGossipHandler(gossipHandler)

	assert.NoError(t, clientNetwork.Connected(nodeID, defaultPeerVersion))

	client := NewClient(clientNetwork)
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

	nodeID := ids.GenerateTestShortID()
	requestID := uint32(1)
	sender := testAppSender{}

	clientNetwork := NewNetwork(sender, codecManager, ids.ShortEmpty, 1)
	clientNetwork.SetGossipHandler(message.NoopMempoolGossipHandler{})
	clientNetwork.SetRequestHandler(&testRequestHandler{})

	assert.NoError(t, clientNetwork.Connected(nodeID, defaultPeerVersion))

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
	assert.NoError(t, clientNetwork.AppGossip(nodeID, gossipMsg))
	assert.NoError(t, clientNetwork.AppGossip(nodeID, requestMessage))
	assert.NoError(t, clientNetwork.AppGossip(nodeID, garbageResponse))
	assert.NoError(t, clientNetwork.AppGossip(nodeID, emptyResponse))
	assert.NoError(t, clientNetwork.AppGossip(nodeID, nilResponse))
	assert.NoError(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), gossipMsg))
	assert.NoError(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), requestMessage))
	assert.NoError(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), garbageResponse))
	assert.NoError(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), emptyResponse))
	assert.NoError(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), nilResponse))
	assert.NoError(t, clientNetwork.AppResponse(nodeID, requestID, gossipMsg))
	assert.NoError(t, clientNetwork.AppResponse(nodeID, requestID, requestMessage))
	assert.NoError(t, clientNetwork.AppResponse(nodeID, requestID, garbageResponse))
	assert.NoError(t, clientNetwork.AppResponse(nodeID, requestID, emptyResponse))
	assert.NoError(t, clientNetwork.AppResponse(nodeID, requestID, nilResponse))
	assert.NoError(t, clientNetwork.AppRequestFailed(nodeID, requestID))
}

func TestNetworkPropagatesRequestHandlerError(t *testing.T) {
	codecManager := buildCodec(t, TestMessage{})

	nodeID := ids.GenerateTestShortID()
	requestID := uint32(1)
	sender := testAppSender{}

	clientNetwork := NewNetwork(sender, codecManager, ids.ShortEmpty, 1)
	clientNetwork.SetGossipHandler(message.NoopMempoolGossipHandler{})
	clientNetwork.SetRequestHandler(&testRequestHandler{err: errors.New("fail")}) // Return an error from the request handler

	assert.NoError(t, clientNetwork.Connected(nodeID, defaultPeerVersion))

	defer clientNetwork.Shutdown()

	// Ensure a valid request message sent as any App specific message type does not trigger a fatal error
	requestMessage, err := marshalStruct(codecManager, TestMessage{Message: "Hello"})
	assert.NoError(t, err)

	// Check that if the request handler returns an error, it is propagated as a fatal error.
	assert.Error(t, clientNetwork.AppRequest(nodeID, requestID, time.Now().Add(time.Second), requestMessage))
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

func buildGossip(codec codec.Manager, msg message.Message) ([]byte, error) {
	return codec.Marshal(message.Version, &msg)
}

type testAppSender struct {
	sendAppRequestFn  func(ids.ShortSet, uint32, []byte) error
	sendAppResponseFn func(ids.ShortID, uint32, []byte) error
	sendAppGossipFn   func([]byte) error
}

func (t testAppSender) SendAppGossipSpecific(ids.ShortSet, []byte) error {
	panic("not implemented")
}

func (t testAppSender) SendAppRequest(nodeIDs ids.ShortSet, requestID uint32, message []byte) error {
	return t.sendAppRequestFn(nodeIDs, requestID, message)
}

func (t testAppSender) SendAppResponse(nodeID ids.ShortID, requestID uint32, message []byte) error {
	return t.sendAppResponseFn(nodeID, requestID, message)
}

func (t testAppSender) SendAppGossip(message []byte) error {
	return t.sendAppGossipFn(message)
}

type HelloRequest struct {
	Message string `serialize:"true"`
}

func (h HelloRequest) Handle(ctx context.Context, nodeID ids.ShortID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	// casting is only necessary for test since RequestHandler does not implement anything at the moment
	return handler.(TestRequestHandler).HandleHelloRequest(ctx, nodeID, requestID, &h)
}

func (h HelloRequest) Type() string {
	return "hello-request"
}

type GreetingRequest struct {
	Greeting string `serialize:"true"`
}

func (g GreetingRequest) Handle(ctx context.Context, nodeID ids.ShortID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	// casting is only necessary for test since RequestHandler does not implement anything at the moment
	return handler.(TestRequestHandler).HandleGreetingRequest(ctx, nodeID, requestID, &g)
}

func (g GreetingRequest) Type() string {
	return "greeting-request"
}

type HelloResponse struct {
	Response string `serialize:"true"`
}

type GreetingResponse struct {
	Greet string `serialize:"true"`
}

type TestRequestHandler interface {
	HandleHelloRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, request *HelloRequest) ([]byte, error)
	HandleGreetingRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, request *GreetingRequest) ([]byte, error)
}

type HelloGreetingRequestHandler struct {
	codec codec.Manager
}

func (h *HelloGreetingRequestHandler) HandleHelloRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, request *HelloRequest) ([]byte, error) {
	return h.codec.Marshal(message.Version, HelloResponse{Response: "Hi"})
}

func (h *HelloGreetingRequestHandler) HandleGreetingRequest(ctx context.Context, nodeID ids.ShortID, requestID uint32, request *GreetingRequest) ([]byte, error) {
	return h.codec.Marshal(message.Version, GreetingResponse{Greet: "Hey there"})
}

type TestMessage struct {
	Message string `serialize:"true"`
}

func (t TestMessage) Handle(ctx context.Context, nodeID ids.ShortID, requestID uint32, handler message.RequestHandler) ([]byte, error) {
	return handler.(*testRequestHandler).handleTestRequest(ctx, nodeID, requestID, &t)
}

func (t TestMessage) Type() string {
	return "test-message"
}

type HelloGossip struct {
	message.Message
	Msg string `serialize:"true"`
}

func (h HelloGossip) Handle(handler message.GossipHandler, nodeID ids.ShortID) error {
	return handler.HandleTxs(nodeID, nil)
}

func (h HelloGossip) Type() string {
	return "hello-gossip"
}

func (h HelloGossip) initialize(_ []byte) {
	// no op
}

func (h HelloGossip) Bytes() []byte {
	// no op
	return nil
}

type testGossipHandler struct {
	received bool
	nodeID   ids.ShortID
	msg      []byte
}

func (t *testGossipHandler) HandleTxs(nodeID ids.ShortID, _ *message.Txs) error {
	t.received = true
	t.nodeID = nodeID
	return nil
}

type testRequestHandler struct {
	calls              uint32
	processingDuration time.Duration
	response           []byte
	err                error
}

func (r *testRequestHandler) handleTestRequest(ctx context.Context, _ ids.ShortID, _ uint32, _ *TestMessage) ([]byte, error) {
	r.calls++
	select {
	case <-time.After(r.processingDuration):
		break
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.response, r.err
}
