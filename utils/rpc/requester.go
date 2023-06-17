// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"context"
	"net/http"
	"net/url"

	"golang.org/x/exp/maps"
)

var _ EndpointRequester = (*avalancheEndpointRequester)(nil)

type EndpointRequester interface {
	SendRequest(ctx context.Context, method string, params interface{}, reply interface{}, options ...Option) error
}

type avalancheEndpointRequester struct {
	uri     string
	cookies map[string]*http.Cookie
}

func NewEndpointRequester(uri string) EndpointRequester {
	return &avalancheEndpointRequester{
		uri:     uri,
		cookies: map[string]*http.Cookie{},
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

	newCookies, err := SendJSONRequest(
		ctx,
		uri,
		method,
		params,
		reply,
		maps.Values(e.cookies),
		options...,
	)

	for _, c := range newCookies {
		e.cookies[c.Name] = c
	}

	return err
}
