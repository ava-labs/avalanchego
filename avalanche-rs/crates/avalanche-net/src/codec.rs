//! Message framing codec for P2P communication.
//!
//! Messages are framed with a 4-byte big-endian length prefix:
//! ```text
//! +----------------+------------------+
//! | Length (4 bytes) | Payload (N bytes) |
//! +----------------+------------------+
//! ```

use bytes::{Buf, BufMut, Bytes, BytesMut};
use tokio_util::codec::{Decoder, Encoder};

use avalanche_proto::{Message, MessageError};

/// Maximum message size (2 MB).
pub const MAX_MESSAGE_SIZE: usize = 2 * 1024 * 1024;

/// A framed message with length prefix.
#[derive(Debug, Clone)]
pub struct MessageFrame {
    /// The message content.
    pub message: Message,
    /// Original encoded size (for metrics).
    pub encoded_size: usize,
    /// Bytes saved by compression (if any).
    pub bytes_saved: usize,
}

impl MessageFrame {
    /// Creates a new message frame.
    #[must_use]
    pub fn new(message: Message) -> Self {
        Self {
            message,
            encoded_size: 0,
            bytes_saved: 0,
        }
    }

    /// Creates a frame with size information.
    #[must_use]
    pub fn with_sizes(message: Message, encoded_size: usize, bytes_saved: usize) -> Self {
        Self {
            message,
            encoded_size,
            bytes_saved,
        }
    }
}

/// Codec for encoding/decoding length-prefixed protobuf messages.
#[derive(Debug, Clone)]
pub struct MessageCodec {
    /// Maximum allowed message size.
    max_size: usize,
    /// State for partial reads.
    state: DecodeState,
}

#[derive(Debug, Clone, Copy)]
enum DecodeState {
    /// Reading the 4-byte length prefix.
    ReadingLength,
    /// Reading the message body of the given length.
    ReadingBody(usize),
}

impl Default for MessageCodec {
    fn default() -> Self {
        Self::new()
    }
}

impl MessageCodec {
    /// Creates a new codec with default settings.
    #[must_use]
    pub fn new() -> Self {
        Self {
            max_size: MAX_MESSAGE_SIZE,
            state: DecodeState::ReadingLength,
        }
    }

    /// Creates a new codec with a custom max size.
    #[must_use]
    pub fn with_max_size(max_size: usize) -> Self {
        Self {
            max_size,
            state: DecodeState::ReadingLength,
        }
    }
}

impl Decoder for MessageCodec {
    type Item = MessageFrame;
    type Error = std::io::Error;

    fn decode(&mut self, src: &mut BytesMut) -> Result<Option<Self::Item>, Self::Error> {
        loop {
            match self.state {
                DecodeState::ReadingLength => {
                    if src.len() < 4 {
                        return Ok(None);
                    }

                    let length = u32::from_be_bytes([src[0], src[1], src[2], src[3]]) as usize;
                    src.advance(4);

                    if length > self.max_size {
                        return Err(std::io::Error::new(
                            std::io::ErrorKind::InvalidData,
                            format!(
                                "message too large: {} bytes (max {})",
                                length, self.max_size
                            ),
                        ));
                    }

                    if length == 0 {
                        return Err(std::io::Error::new(
                            std::io::ErrorKind::InvalidData,
                            "empty message",
                        ));
                    }

                    self.state = DecodeState::ReadingBody(length);
                }
                DecodeState::ReadingBody(length) => {
                    if src.len() < length {
                        return Ok(None);
                    }

                    let data = src.split_to(length);
                    self.state = DecodeState::ReadingLength;

                    // Decode the message
                    let message = Message::decode(&data).map_err(|e| {
                        std::io::Error::new(std::io::ErrorKind::InvalidData, e.to_string())
                    })?;

                    // Decompress if needed
                    let (decompressed, bytes_saved) =
                        if matches!(message, Message::CompressedZstd(_)) {
                            let original_size = data.len();
                            let msg = message.decompress().map_err(|e| {
                                std::io::Error::new(std::io::ErrorKind::InvalidData, e.to_string())
                            })?;
                            let decompressed_size = msg.encode().map(|b| b.len()).unwrap_or(0);
                            (msg, decompressed_size.saturating_sub(original_size))
                        } else {
                            (message, 0)
                        };

                    return Ok(Some(MessageFrame::with_sizes(
                        decompressed,
                        length,
                        bytes_saved,
                    )));
                }
            }
        }
    }
}

impl Encoder<MessageFrame> for MessageCodec {
    type Error = std::io::Error;

    fn encode(&mut self, item: MessageFrame, dst: &mut BytesMut) -> Result<(), Self::Error> {
        // Try to compress the message
        let message = item.message.compress().map_err(|e| {
            std::io::Error::new(std::io::ErrorKind::InvalidData, e.to_string())
        })?;

        // Encode the message
        let encoded = message.encode().map_err(|e| {
            std::io::Error::new(std::io::ErrorKind::InvalidData, e.to_string())
        })?;

        let length = encoded.len();
        if length > self.max_size {
            return Err(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                format!("message too large: {} bytes (max {})", length, self.max_size),
            ));
        }

        // Write length prefix
        dst.put_u32(length as u32);
        // Write message body
        dst.put_slice(&encoded);

        Ok(())
    }
}

/// Encodes a message to bytes with length prefix.
pub fn encode_message(message: &Message) -> Result<Bytes, MessageError> {
    let compressed = message.compress()?;
    let encoded = compressed.encode()?;

    let mut buf = BytesMut::with_capacity(4 + encoded.len());
    buf.put_u32(encoded.len() as u32);
    buf.put_slice(&encoded);

    Ok(buf.freeze())
}

/// Decodes a message from bytes with length prefix.
pub fn decode_message(mut data: &[u8]) -> Result<Message, MessageError> {
    if data.len() < 4 {
        return Err(MessageError::InsufficientData {
            needed: 4,
            available: data.len(),
        });
    }

    let length = u32::from_be_bytes([data[0], data[1], data[2], data[3]]) as usize;
    data = &data[4..];

    if data.len() < length {
        return Err(MessageError::InsufficientData {
            needed: length,
            available: data.len(),
        });
    }

    let message = Message::decode(&data[..length])?;
    message.decompress()
}

#[cfg(test)]
mod tests {
    use super::*;
    use avalanche_proto::{Ping, Pong};
    use tokio_util::codec::{Decoder, Encoder};

    #[test]
    fn test_encode_decode_roundtrip() {
        let mut codec = MessageCodec::new();
        let mut buf = BytesMut::new();

        // Encode
        let frame = MessageFrame::new(Message::Ping(Ping { uptime: 95 }));
        codec.encode(frame, &mut buf).unwrap();

        // Decode
        let decoded = codec.decode(&mut buf).unwrap().unwrap();
        assert_eq!(decoded.message, Message::Ping(Ping { uptime: 95 }));
    }

    #[test]
    fn test_partial_read() {
        let mut codec = MessageCodec::new();
        let mut buf = BytesMut::new();

        // Encode a message
        let frame = MessageFrame::new(Message::Pong(Pong {}));
        codec.encode(frame, &mut buf).unwrap();

        // Split the buffer to simulate partial reads
        let total = buf.len();
        let first_half = buf.split_to(total / 2);
        let second_half = buf;

        // Try to decode with partial data
        let mut partial_buf = BytesMut::from(first_half.as_ref());
        let result = codec.decode(&mut partial_buf).unwrap();
        assert!(result.is_none());

        // Complete the data
        partial_buf.extend_from_slice(&second_half);
        let decoded = codec.decode(&mut partial_buf).unwrap().unwrap();
        assert_eq!(decoded.message, Message::Pong(Pong {}));
    }

    #[test]
    fn test_message_too_large() {
        let mut codec = MessageCodec::with_max_size(10);
        let mut buf = BytesMut::new();

        // Write a length that's too large
        buf.put_u32(1000);
        buf.extend_from_slice(&[0u8; 100]);

        let result = codec.decode(&mut buf);
        assert!(result.is_err());
    }

    #[test]
    fn test_empty_message() {
        let mut codec = MessageCodec::new();
        let mut buf = BytesMut::new();

        // Write zero length
        buf.put_u32(0);

        let result = codec.decode(&mut buf);
        assert!(result.is_err());
    }

    #[test]
    fn test_encode_message_helper() {
        let msg = Message::Ping(Ping { uptime: 50 });
        let encoded = encode_message(&msg).unwrap();

        assert!(encoded.len() > 4); // At least length prefix
        assert_eq!(
            u32::from_be_bytes([encoded[0], encoded[1], encoded[2], encoded[3]]) as usize,
            encoded.len() - 4
        );
    }

    #[test]
    fn test_decode_message_helper() {
        let msg = Message::Ping(Ping { uptime: 75 });
        let encoded = encode_message(&msg).unwrap();
        let decoded = decode_message(&encoded).unwrap();

        assert_eq!(decoded, msg);
    }
}
