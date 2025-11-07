// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"net/http"
	"net/url"
)

var (
	_ EndpointRequester = (*avalancheEndpointRequester)(nil)
	_ client            = (*httpClient)(nil)
)

type EndpointRequester interface {
	SendRequest(ctx context.Context, method string, params interface{}, reply interface{}, options ...Option) error
}

type avalancheEndpointRequester struct {
	client client
	uri    string
}

func NewEndpointRequester(uri string) EndpointRequester {
	return &avalancheEndpointRequester{
		client: &httpClient{c: http.DefaultClient},
		uri:    uri,
	}
}

func (e *avalancheEndpointRequester) SendRequest(
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
		e.client,
		ctx,
		uri,
		method,
		params,
		reply,
		options...,
	)
}

type client interface {
	Send(req *http.Request) (*http.Response, error)
}

type httpClient struct {
	c *http.Client
}

func (h httpClient) Send(req *http.Request) (*http.Response, error) {
	return h.c.Do(req)
}
