// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"errors"

	"github.com/ava-labs/subnet-evm/plugin/evm/message"
)

var (
	_ message.ResponseHandler = (*waitingResponseHandler)(nil)

	errRequestFailed = errors.New("request failed")
)

// waitingResponseHandler implements the ResponseHandler interface
// Internally used to wait for response after making a request synchronously
// responseChan may contain response bytes if the original request has not failed
// responseChan is closed in either fail or success scenario
type waitingResponseHandler struct {
	responseChan chan []byte // blocking channel with response bytes
	failed       bool        // whether the original request is failed
}

// newWaitingResponseHandler returns new instance of the waitingResponseHandler
func newWaitingResponseHandler() *waitingResponseHandler {
	return &waitingResponseHandler{
		// Make buffer length 1 so that OnResponse can complete
		// even if no goroutine is waiting on the channel (i.e.
		// the context of a request is cancelled.)
		responseChan: make(chan []byte, 1),
	}
}

// OnResponse passes the response bytes to the responseChan and closes the channel
func (w *waitingResponseHandler) OnResponse(response []byte) error {
	w.responseChan <- response
	close(w.responseChan)
	return nil
}

// OnFailure sets the failed flag to true and closes the channel
func (w *waitingResponseHandler) OnFailure() error {
	w.failed = true
	close(w.responseChan)
	return nil
}

func (waitingHandler *waitingResponseHandler) WaitForResult(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-waitingHandler.responseChan:
		if waitingHandler.failed {
			return nil, errRequestFailed
		}
		return response, nil
	}
}
