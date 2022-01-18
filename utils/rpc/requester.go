// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	rpc "github.com/gorilla/rpc/v2/json2"
)

type Requester interface {
	SendJSONRPCRequest(ctx context.Context, endpoint string, method string, params interface{}, reply interface{}) error
}

type jsonRPCRequester struct {
	uri    string
	client http.Client
}

func NewRPCRequester(uri string) Requester {
	return &jsonRPCRequester{
		uri:    uri,
		client: *http.DefaultClient,
	}
}

func (requester jsonRPCRequester) SendJSONRPCRequest(ctx context.Context, endpoint string, method string, params interface{}, reply interface{}) error {
	// Golang has a nasty & subtle behaviour where duplicated '//' in the URL is treated as GET, even if it's POST
	// https://stackoverflow.com/questions/23463601/why-golang-treats-my-post-request-as-a-get-one
	endpoint = strings.TrimLeft(endpoint, "/")

	requestBodyBytes, err := rpc.EncodeClientRequest(method, params)
	if err != nil {
		return fmt.Errorf("problem marshaling request to endpoint '%v' with method '%v' and params '%v': %w", endpoint, method, params, err)
	}

	url := fmt.Sprintf("%v/%v", requester.uri, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		return fmt.Errorf("problem while creating JSON RPC POST request to %s: %s", url, err)
	}

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
	SendRequest(ctx context.Context, method string, params interface{}, reply interface{}) error
}

type avalancheEndpointRequester struct {
	requester      Requester
	endpoint, base string
}

func NewEndpointRequester(uri, endpoint, base string) EndpointRequester {
	return &avalancheEndpointRequester{
		requester: NewRPCRequester(uri),
		endpoint:  endpoint,
		base:      base,
	}
}

func (e *avalancheEndpointRequester) SendRequest(ctx context.Context, method string, params interface{}, reply interface{}) error {
	return e.requester.SendJSONRPCRequest(
		ctx,
		e.endpoint,
		fmt.Sprintf("%s.%s", e.base, method),
		params,
		reply,
	)
}
