package greader

import (
	"context"
	"io"
	"net"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/proto/pb/io/reader"
)

// TestEOF tests that if an io.EOF is returned by an io.Reader, it propagates
// the same error type. This is important because the io.EOF type is used as a
// sentinel error value for a lot of things.
func TestEOF(t *testing.T) {
	require := require.New(t)

	server := grpc.NewServer()
	listener := bufconn.Listen(1024)

	readerServer := NewServer(iotest.ErrReader(io.EOF))
	reader.RegisterReaderServer(server, readerServer)

	go func() {
		require.NoError(server.Serve(listener))
	}()

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(err)

	client := NewClient(reader.NewReaderClient(conn))

	buf := make([]byte, 1)
	n, err := client.Read(buf)
	require.Zero(n)
	require.ErrorIs(err, io.EOF)
}
