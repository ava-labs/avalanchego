// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	rpc "github.com/gorilla/rpc/v2/json2"
)

type requester interface {
	sendJSONRPCRequest(
		ctx context.Context,
		endpoint string,
		headers http.Header,
		queryParams url.Values,
		method string,
		params interface{},
		reply interface{},
	) error
}

type jsonRPCRequester struct {
	uri    string
	client http.Client
}

func newRPCRequester(uri string) requester {
	return &jsonRPCRequester{
		uri:    uri,
		client: *http.DefaultClient,
	}
}

func (requester jsonRPCRequester) sendJSONRPCRequest(
	ctx context.Context,
	endpoint string,
	headers http.Header,
	queryParams url.Values,
	method string,
	params interface{},
	reply interface{},
) error {
	requestBodyBytes, err := rpc.EncodeClientRequest(method, params)
	if err != nil {
		return fmt.Errorf("problem marshaling request to endpoint '%v' with method '%v' and params '%v': %w", endpoint, method, params, err)
	}

	queryParamsStr := queryParams.Encode()
	if len(queryParamsStr) > 0 {
		queryParamsStr = "?" + queryParamsStr
	}

	url := fmt.Sprintf("%s%s%s", requester.uri, endpoint, queryParamsStr)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return fmt.Errorf("problem while creating JSON RPC POST request to %s: %s", url, err)
	}

	req.Header = headers
	req.Header.Set("Content-Type", "application/json")

	resp, err := requester.client.Do(req)
	if err != nil {
		return fmt.Errorf("problem while making JSON RPC POST request to %s: %w", url, err)
	}
	statusCode := resp.StatusCode

	// Return an error for any non successful status code
	if statusCode < 200 || statusCode > 299 {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("received status code '%v'", statusCode)
	}

	if err := rpc.DecodeClientResponse(resp.Body, reply); err != nil {
		return err
	}
	return resp.Body.Close()
}

type EndpointRequester interface {
	SendRequest(ctx context.Context, method string, params interface{}, reply interface{}, options ...Option) error
}

type avalancheEndpointRequester struct {
	requester      requester
	endpoint, base string
}

func NewEndpointRequester(uri, endpoint, base string) EndpointRequester {
	return &avalancheEndpointRequester{
		requester: newRPCRequester(uri),
		endpoint:  endpoint,
		base:      base,
	}
}

func (e *avalancheEndpointRequester) SendRequest(
	ctx context.Context,
	method string,
	params interface{},
	reply interface{},
	options ...Option,
) error {
	ops := NewOptions(options)
	return e.requester.sendJSONRPCRequest(
		ctx,
		e.endpoint,
		ops.Headers(),
		ops.QueryParams(),
		fmt.Sprintf("%s.%s", e.base, method),
		params,
		reply,
	)
}
