// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

var errInvalidNumberOfChecks = errors.New("expected at least 1 check attempt")

// Client for Avalanche Health API Endpoint
type Client struct {
	requester rpc.EndpointRequester
}

// Result represents the output of a health check execution.
type Result struct {
	// the details of task Result - may be nil
	Details interface{} `json:"message,omitempty"`
	// the error returned from a failed health check - an empty string when successful
	Error string `json:"error,omitempty"`
	// the time of the last health check
	Timestamp time.Time `json:"timestamp"`
	// the execution duration of the last check
	Duration time.Duration `json:"duration,omitempty"`
	// the number of failures that occurred in a row
	ContiguousFailures int64 `json:"contiguousFailures"`
	// the time of the initial transitional failure
	TimeOfFirstFailure *time.Time `json:"timeOfFirstFailure"`
}

type APIHealthClientReply struct {
	Checks  map[string]Result `json:"checks"`
	Healthy bool              `json:"healthy"`
}

// NewClient returns a client to interact with Health API endpoint
func NewClient(uri string, requestTimeout time.Duration) *Client {
	return &Client{
		requester: rpc.NewEndpointRequester(uri, "/ext/health", "health", requestTimeout),
	}
}

// Health returns a health check on the Avalanche node
func (c *Client) Health() (*APIHealthClientReply, error) {
	res := &APIHealthClientReply{}
	err := c.requester.SendRequest("health", struct{}{}, res)
	return res, err
}

// AwaitHealthy queries the Health endpoint [checks] times, with a pause of
// [interval] in between checks and returns early if Health returns healthy
func (c *Client) AwaitHealthy(checks int, interval time.Duration) (bool, error) {
	if checks < 1 {
		return false, errInvalidNumberOfChecks
	}

	// Check health once outside the loop to avoid sleeping unnecessarily.
	res, err := c.Health()
	if err == nil && res.Healthy {
		return true, nil
	}

	for i := 1; i < checks; i++ {
		time.Sleep(interval)

		res, err = c.Health()
		if err == nil && res.Healthy {
			return true, nil
		}
	}
	return false, err
}
