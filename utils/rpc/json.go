// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	rpc "github.com/gorilla/rpc/v2/json2"
)

// CleanlyCloseBody avoids sending unnecessary RST_STREAM and PING frames by
// ensuring the whole body is read before being closed.
// See https://blog.cloudflare.com/go-and-enhance-your-calm/#reading-bodies-in-go-can-be-unintuitive
func CleanlyCloseBody(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func SendJSONRequest(
	ctx context.Context,
	uri *url.URL,
	method string,
	params interface{},
	reply interface{},
	options ...Option,
) error {
	requestBodyBytes, err := rpc.EncodeClientRequest(method, params)
	if err != nil {
		return fmt.Errorf("failed to encode client params: %w", err)
	}

	ops := NewOptions(options)
	uri.RawQuery = ops.queryParams.Encode()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		uri.String(),
		bytes.NewBuffer(requestBodyBytes),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	request.Header = ops.headers
	request.Header.Set("Content-Type", "application/json")

	//nolint:bodyclose // body is closed via CleanlyCloseBody
	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to issue request: %w", err)
	}
	defer CleanlyCloseBody(resp.Body)

	// Return an error for any non successful status code
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	if err := rpc.DecodeClientResponse(resp.Body, reply); err != nil {
		return fmt.Errorf("failed to decode client response: %w", err)
	}

	return nil
}
