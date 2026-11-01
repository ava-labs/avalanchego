// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

type echoService struct{}

func (echoService) Echo(_ *http.Request, args *string, reply *string) error {
	*reply = *args
	return nil
}

func TestNewHandler(t *testing.T) {
	handler, err := NewHandler("echo", echoService{})
	require.NoError(t, err, "NewHandler()")

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	const msg = "austin, ive been trying to reach you about your car's extended warranty"

	var reply string
	requester := NewEndpointRequester(server.URL)
	err = requester.SendRequest(t.Context(), "echo.echo", msg, &reply)
	require.NoErrorf(t, err, "%T.SendRequest()", requester)
	require.Equalf(t, msg, reply, "%T.SendRequest() reply", requester)
}
