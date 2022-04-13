// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"net"
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/stretchr/testify/assert"

	"github.com/chain4travel/caminogo/api/proto/galiasreaderproto"
	"github.com/chain4travel/caminogo/ids"
)

const (
	bufSize = 1024 * 1024
)

func TestInterface(t *testing.T) {
	assert := assert.New(t)
	for _, test := range ids.AliasTests {
		listener := bufconn.Listen(bufSize)
		server := grpc.NewServer()
		w := ids.NewAliaser()
		galiasreaderproto.RegisterAliasReaderServer(server, NewServer(w))
		go func() {
			if err := server.Serve(listener); err != nil {
				t.Logf("Server exited with error: %v", err)
			}
		}()

		dialer := grpc.WithContextDialer(
			func(context.Context, string) (net.Conn, error) {
				return listener.Dial()
			},
		)

		ctx := context.Background()
		conn, err := grpc.DialContext(ctx, "", dialer, grpc.WithInsecure())
		assert.NoError(err)

		r := NewClient(galiasreaderproto.NewAliasReaderClient(conn))
		test(assert, r, w)

		server.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
}
