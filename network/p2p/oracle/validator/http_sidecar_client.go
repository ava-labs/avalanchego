// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ava-labs/avalanchego/network/p2p/oracle"
)

var _ oracle.SidecarClient = (*HTTPSidecarClient)(nil)

// verifyRequest is the JSON body sent to the sidecar's /verify endpoint.
// Fields match the proto definition in sidecar.proto.
type verifyRequest struct {
	// MessageBytes is the hex-encoded canonical codec encoding of OracleMessage.
	MessageBytes string `json:"message_bytes"`
	// Justification is hex-encoded lookup hints from the relayer (e.g. a
	// Solana transaction signature). Not part of the signed output.
	Justification string `json:"justification"`
}

// verifyResponse is the JSON body returned by the sidecar on success.
// A non-2xx HTTP status signals failure; the body carries the error message.
type verifyResponse struct {
	// Error is non-empty when the sidecar rejects the event.
	Error string `json:"error,omitempty"`
}

// HTTPSidecarClient calls a sidecar process over HTTP/JSON. It is the
// production implementation of SidecarClient.
//
// The sidecar must expose POST /verify accepting a verifyRequest body and
// returning a verifyResponse body. Use 200 OK for success, 400 for invalid
// events, 503 for source-chain unavailability.
type HTTPSidecarClient struct {
	endpoint   string // e.g. "http://127.0.0.1:9900"
	httpClient *http.Client
}

func NewHTTPSidecarClient(endpoint string, httpClient *http.Client) *HTTPSidecarClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &HTTPSidecarClient{
		endpoint:   endpoint,
		httpClient: httpClient,
	}
}

func (c *HTTPSidecarClient) Verify(ctx context.Context, event *oracle.OracleEvent) error {
	body, err := json.Marshal(verifyRequest{
		MessageBytes:  hex.EncodeToString(event.Message.Bytes()),
		Justification: hex.EncodeToString(event.Justification),
	})
	if err != nil {
		return fmt.Errorf("failed to marshal verify request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/verify", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to build verify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sidecar unreachable: %w", err)
	}
	defer resp.Body.Close()

	var result verifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode sidecar response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		msg := result.Error
		if msg == "" {
			msg = fmt.Sprintf("sidecar returned HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("sidecar rejected event: %s", msg)
	}

	return nil
}
