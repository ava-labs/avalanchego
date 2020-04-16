// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"io/ioutil"
	"net/http"

	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/proto"
)

// Client is an implementation of a messenger channel that talks over RPC.
type Client struct{ client proto.HTTPClient }

// NewClient returns a database instance connected to a remote database instance
func NewClient(client proto.HTTPClient) *Client {
	return &Client{client: client}
}

// Handle ...
func (c *Client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// get the headers
	inboundHeaders := make([]*proto.Header, len(r.Header))[:0]
	for key, values := range r.Header {
		inboundHeaders = append(inboundHeaders, &proto.Header{
			Key:    key,
			Values: values,
		})
	}

	// get the body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// execute the call
	resp, err := c.client.Handle(r.Context(), &proto.HTTPRequest{
		Url:     r.RequestURI,
		Method:  r.Method,
		Headers: inboundHeaders,
		Body:    body,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// return the code
	w.WriteHeader(int(resp.Code))

	// return the headers
	outboundHeaders := w.Header()
	for _, header := range resp.Headers {
		outboundHeaders[header.Key] = header.Values
	}

	// return the body
	if _, err := w.Write(resp.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
