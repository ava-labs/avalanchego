//! TCP connection handling with TLS support.

use std::net::SocketAddr;

use bytes::Bytes;
use futures::stream::{SplitSink, SplitStream};
use futures::{SinkExt, StreamExt};
use tokio::io::{AsyncRead, AsyncWrite};
use tokio::net::TcpStream;
use tokio::sync::mpsc;
use tokio_rustls::client::TlsStream as ClientTlsStream;
use tokio_rustls::server::TlsStream as ServerTlsStream;
use tokio_util::codec::{Framed, LengthDelimitedCodec};
use tracing::{debug, error, warn};

use avalanche_ids::NodeId;

use crate::{NetworkError, Result};

/// Maximum message size (16 MB).
const MAX_MESSAGE_SIZE: usize = 16 * 1024 * 1024;

/// Channel buffer size for outbound messages.
const OUTBOUND_BUFFER_SIZE: usize = 1024;

/// An established connection to a peer.
pub struct Connection {
    /// Remote peer's node ID.
    pub node_id: NodeId,
    /// Remote address.
    pub addr: SocketAddr,
    /// Whether this is an inbound connection.
    pub inbound: bool,
    /// Sender for outbound messages (public for cloning).
    pub outbound_tx: mpsc::Sender<Bytes>,
    /// Receiver for inbound messages.
    inbound_rx: mpsc::Receiver<Bytes>,
    /// Handle to the reader task.
    reader_handle: tokio::task::JoinHandle<()>,
    /// Handle to the writer task.
    writer_handle: tokio::task::JoinHandle<()>,
}

impl Connection {
    /// Creates a new connection from a client TLS stream (outbound).
    pub fn from_client_tls(
        stream: ClientTlsStream<TcpStream>,
        node_id: NodeId,
        addr: SocketAddr,
    ) -> Self {
        Self::new_internal(stream, node_id, addr, false)
    }

    /// Creates a new connection from a server TLS stream (inbound).
    pub fn from_server_tls(
        stream: ServerTlsStream<TcpStream>,
        node_id: NodeId,
        addr: SocketAddr,
    ) -> Self {
        Self::new_internal(stream, node_id, addr, true)
    }

    /// Creates a new connection from a plain TCP stream (for testing).
    pub fn from_tcp(stream: TcpStream, node_id: NodeId, addr: SocketAddr, inbound: bool) -> Self {
        Self::new_internal(stream, node_id, addr, inbound)
    }

    fn new_internal<S>(stream: S, node_id: NodeId, addr: SocketAddr, inbound: bool) -> Self
    where
        S: AsyncRead + AsyncWrite + Unpin + Send + 'static,
    {
        // Configure length-delimited framing
        let codec = LengthDelimitedCodec::builder()
            .length_field_offset(0)
            .length_field_length(4)
            .length_adjustment(0)
            .max_frame_length(MAX_MESSAGE_SIZE)
            .new_codec();

        let framed = Framed::new(stream, codec);
        let (sink, stream) = framed.split();

        // Create channels
        let (outbound_tx, outbound_rx) = mpsc::channel(OUTBOUND_BUFFER_SIZE);
        let (inbound_tx, inbound_rx) = mpsc::channel(OUTBOUND_BUFFER_SIZE);

        // Spawn reader task
        let reader_handle = tokio::spawn(Self::read_loop(stream, inbound_tx, node_id));

        // Spawn writer task
        let writer_handle = tokio::spawn(Self::write_loop(sink, outbound_rx, node_id));

        Self {
            node_id,
            addr,
            inbound,
            outbound_tx,
            inbound_rx,
            reader_handle,
            writer_handle,
        }
    }

    async fn read_loop<S>(
        mut stream: SplitStream<Framed<S, LengthDelimitedCodec>>,
        tx: mpsc::Sender<Bytes>,
        node_id: NodeId,
    ) where
        S: AsyncRead + Unpin,
    {
        loop {
            match stream.next().await {
                Some(Ok(bytes)) => {
                    if tx.send(bytes.freeze()).await.is_err() {
                        debug!("Connection closed, stopping reader for {}", node_id);
                        break;
                    }
                }
                Some(Err(e)) => {
                    warn!("Read error from {}: {}", node_id, e);
                    break;
                }
                None => {
                    debug!("Connection closed by {}", node_id);
                    break;
                }
            }
        }
    }

    async fn write_loop<S>(
        mut sink: SplitSink<Framed<S, LengthDelimitedCodec>, Bytes>,
        mut rx: mpsc::Receiver<Bytes>,
        node_id: NodeId,
    ) where
        S: AsyncWrite + Unpin,
    {
        while let Some(bytes) = rx.recv().await {
            if let Err(e) = sink.send(bytes).await {
                error!("Write error to {}: {}", node_id, e);
                break;
            }
        }
        debug!("Writer stopped for {}", node_id);
    }

    /// Sends a message to the peer.
    pub async fn send(&self, data: Bytes) -> Result<()> {
        self.outbound_tx
            .send(data)
            .await
            .map_err(|_| NetworkError::Closed)
    }

    /// Receives a message from the peer.
    pub async fn recv(&mut self) -> Option<Bytes> {
        self.inbound_rx.recv().await
    }

    /// Closes the connection.
    pub async fn close(self) {
        drop(self.outbound_tx);
        self.writer_handle.abort();
        self.reader_handle.abort();
    }

    /// Checks if the connection is still alive.
    pub fn is_alive(&self) -> bool {
        !self.reader_handle.is_finished() && !self.writer_handle.is_finished()
    }
}

/// Connection statistics.
#[derive(Debug, Clone, Default)]
pub struct ConnectionStats {
    /// Bytes sent.
    pub bytes_sent: u64,
    /// Bytes received.
    pub bytes_received: u64,
    /// Messages sent.
    pub messages_sent: u64,
    /// Messages received.
    pub messages_received: u64,
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::net::TcpListener;

    #[tokio::test]
    async fn test_connection_send_recv() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let server_task = tokio::spawn(async move {
            let (stream, _) = listener.accept().await.unwrap();
            let mut conn = Connection::from_tcp(stream, NodeId::EMPTY, addr, true);

            // Receive message
            let data = conn.recv().await.unwrap();
            assert_eq!(&data[..], b"hello");

            // Send response
            conn.send(Bytes::from_static(b"world")).await.unwrap();
        });

        // Client connects
        let stream = TcpStream::connect(addr).await.unwrap();
        let mut conn = Connection::from_tcp(stream, NodeId::EMPTY, addr, false);

        // Send message
        conn.send(Bytes::from_static(b"hello")).await.unwrap();

        // Receive response
        let data = conn.recv().await.unwrap();
        assert_eq!(&data[..], b"world");

        server_task.await.unwrap();
    }
}
