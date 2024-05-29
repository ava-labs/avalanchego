// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

var _ ResponseHandler = (*responseHandler)(nil)

// Handles responses/failure notifications for a sent request.
// Exactly one of OnResponse or OnFailure is eventually called.
type ResponseHandler interface {
	// Called when [response] is received.
	OnResponse(response []byte)
	// Called when the request failed or timed out.
	OnFailure()
}

func newResponseHandler() *responseHandler {
	return &responseHandler{responseChan: make(chan []byte)}
}

// Implements [ResponseHandler].
// Used to wait for a response after making a synchronous request.
// responseChan contains response bytes if the request succeeded.
// responseChan is closed in either fail or success scenario.
type responseHandler struct {
	// If [OnResponse] is called, the response bytes are sent on this channel.
	// If [OnFailure] is called, the channel is closed without sending bytes.
	responseChan chan []byte
}

// OnResponse passes the response bytes to the responseChan and closes the
// channel.
func (h *responseHandler) OnResponse(response []byte) {
	h.responseChan <- response
	close(h.responseChan)
}

// OnFailure closes the channel.
func (h *responseHandler) OnFailure() {
	close(h.responseChan)
}
