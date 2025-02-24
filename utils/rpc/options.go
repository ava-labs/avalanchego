// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"net/http"
	"net/url"
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
