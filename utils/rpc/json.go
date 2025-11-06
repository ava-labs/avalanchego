// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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

	resp, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("failed to issue request: %w", err)
	}

	// Return an error for any non successful status code
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Avoid sending unnecessary RST_STREAM and PING frames by ensuring the whole body is read.
		// See https://blog.cloudflare.com/go-and-enhance-your-calm/#reading-bodies-in-go-can-be-unintuitive
		_, _ = io.Copy(io.Discard, resp.Body)

		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	if err := rpc.DecodeClientResponse(resp.Body, reply); err != nil {
		// Avoid sending unnecessary RST_STREAM and PING frames by ensuring the whole body is read.
		// See https://blog.cloudflare.com/go-and-enhance-your-calm/#reading-bodies-in-go-can-be-unintuitive
		_, _ = io.Copy(io.Discard, resp.Body)

		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("failed to decode client response: %w", err)
	}

	// Avoid sending unnecessary RST_STREAM and PING frames by ensuring the whole body is read.
	// See https://blog.cloudflare.com/go-and-enhance-your-calm/#reading-bodies-in-go-can-be-unintuitive
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.Body.Close()
}
