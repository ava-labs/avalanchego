// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/proto"
)

// Server is a http.Handler that is managed over RPC.
type Server struct{ handler http.Handler }

// NewServer returns a http.Handler instance manage remotely
func NewServer(handler http.Handler) *Server {
	return &Server{handler: handler}
}

// Handle ...
func (s Server) Handle(ctx context.Context, req *proto.HTTPRequest) (*proto.HTTPResponse, error) {
	// create the request with the current context
	r, err := http.NewRequestWithContext(
		ctx,
		req.Method,
		req.Url,
		ioutil.NopCloser(bytes.NewReader(req.Body)),
	)
	if err != nil {
		return nil, err
	}

	// populate the headers
	for _, header := range req.Headers {
		r.Header[header.Key] = header.Values
	}

	// send the request to the recorder
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, r)

	// get the returned headers
	returnedHeaders := recorder.Header()
	headers := make([]*proto.Header, len(returnedHeaders))[:0]
	for key, values := range returnedHeaders {
		headers = append(headers, &proto.Header{
			Key:    key,
			Values: values,
		})
	}

	// return the response
	return &proto.HTTPResponse{
		Code:    int32(recorder.Code),
		Headers: headers,
		Body:    recorder.Body.Bytes(),
	}, nil
}
