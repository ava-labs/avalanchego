// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package galiasreader

import (
	"context"
	"log"
	"net"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ids/galiasreader/galiasreaderproto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

const (
	bufSize = 1024 * 1024
)

func TestInterface(t *testing.T) {
	for _, test := range ids.AliasTests {
		listener := bufconn.Listen(bufSize)
		server := grpc.NewServer()
		w := ids.NewAliaser()
		galiasreaderproto.RegisterAliasReaderServer(server, NewServer(w))
		go func() {
			if err := server.Serve(listener); err != nil {
				log.Fatalf("Server exited with error: %v", err)
			}
		}()

		dialer := grpc.WithContextDialer(
			func(context.Context, string) (net.Conn, error) {
				return listener.Dial()
			},
		)

		ctx := context.Background()
		conn, err := grpc.DialContext(ctx, "", dialer, grpc.WithInsecure())
		if err != nil {
			t.Fatalf("Failed to dial: %s", err)
		}

		r := NewClient(galiasreaderproto.NewAliasReaderClient(conn))
		test(t, r, w)

		server.Stop()
		_ = conn.Close()
		_ = listener.Close()
	}
}
