// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/ids"

	aliasreaderpb "github.com/ava-labs/avalanchego/proto/pb/aliasreader"
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
		aliasreaderpb.RegisterAliasReaderServer(server, NewServer(w))
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

		r := NewClient(aliasreaderpb.NewAliasReaderClient(conn))
		test(assert, r, w)

		server.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
}
