// (c) 2021-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

// TODO danlaine: We create a new response handler for every request.
// Look into making a struct to handle requests/responses that uses a sync pool
// to avoid allocations.

var _ ResponseHandler = &responseHandler{}

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
// responseChan may contain response bytes if the original request has not failed.
// responseChan is closed in either fail or success scenario.
type responseHandler struct {
	// If [OnResponse] is called, the response bytes are sent on this channel.
	responseChan chan []byte
	// Set to true in [OnFailure].
	failed bool
}

// OnResponse passes the response bytes to the responseChan and closes the channel
func (h *responseHandler) OnResponse(response []byte) {
	h.responseChan <- response
	close(h.responseChan)
}

// OnFailure sets the failed flag to true and closes the channel
func (h *responseHandler) OnFailure() {
	h.failed = true
	close(h.responseChan)
}
