// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	rpc "github.com/gorilla/rpc/v2/json2"
)

type Option func(*Options)

type Options struct {
	headers     http.Header
	queryParams url.Values
}

func NewOptions(ops []Option) *Options {
	o := &Options{
		headers:     http.Header{},
		queryParams: url.Values{},
	}
	o.applyOptions(ops)
	return o
}

func (o *Options) applyOptions(ops []Option) {
	for _, op := range ops {
		op(o)
	}
}

func (o *Options) Headers() http.Header {
	return o.headers
}

func (o *Options) QueryParams() url.Values {
	return o.queryParams
}

func WithHeader(key, val string) Option {
	return func(o *Options) {
		o.headers.Set(key, val)
	}
}

func WithQueryParam(key, val string) Option {
	return func(o *Options) {
		o.queryParams.Set(key, val)
	}
}

// EndpointRequester is an extension of AvalancheGo's [EndpointRequester] with
// [http.Client] reuse.
type EndpointRequester struct {
	cli       *http.Client
	uri, base string
}

func NewEndpointRequester(uri, base string) *EndpointRequester {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 100_000
	t.MaxConnsPerHost = 100_000
	t.MaxIdleConnsPerHost = 100_000

	return &EndpointRequester{
		cli: &http.Client{
			Timeout:   3600 * time.Second, // allow waiting requests
			Transport: t,
		},
		uri:  uri,
		base: base,
	}
}

func (e *EndpointRequester) SendRequest(
	ctx context.Context,
	method string,
	params interface{},
	reply interface{},
	options ...Option,
) error {
	uri, err := url.Parse(e.uri)
	if err != nil {
		return err
	}
	return SendJSONRequest(
		ctx,
		e.cli,
		uri,
		fmt.Sprintf("%s.%s", e.base, method),
		params,
		reply,
		options...,
	)
}

func SendJSONRequest(
	ctx context.Context,
	cli *http.Client,
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
		"POST",
		uri.String(),
		bytes.NewBuffer(requestBodyBytes),
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	request.Header = ops.headers
	request.Header.Set("Content-Type", "application/json")

	resp, err := cli.Do(request)
	if err != nil {
		return fmt.Errorf("failed to issue request: %w", err)
	}

	// Return an error for any non successful status code
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("received status code: %d", resp.StatusCode)
	}

	if err := rpc.DecodeClientResponse(resp.Body, reply); err != nil {
		// Drop any error during close to report the original error
		_ = resp.Body.Close()
		return fmt.Errorf("failed to decode client response: %w", err)
	}
	return resp.Body.Close()
}
