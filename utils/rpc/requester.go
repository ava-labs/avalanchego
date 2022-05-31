// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"fmt"
	"net/url"
)

var _ EndpointRequester = &avalancheEndpointRequester{}

type EndpointRequester interface {
	SendRequest(ctx context.Context, method string, params interface{}, reply interface{}, options ...Option) error
}

type avalancheEndpointRequester struct {
	uri, endpoint, base string
}

func NewEndpointRequester(uri, endpoint, base string) EndpointRequester {
	return &avalancheEndpointRequester{
		uri:      uri,
		endpoint: endpoint,
		base:     base,
	}
}

func (e *avalancheEndpointRequester) SendRequest(
	ctx context.Context,
	method string,
	params interface{},
	reply interface{},
	options ...Option,
) error {
	uri, err := url.Parse(fmt.Sprintf("%s%s", e.uri, e.endpoint))
	if err != nil {
		return err
	}
	return SendJSONRequest(
		ctx,
		uri,
		method,
		params,
		reply,
		options...,
	)
}
