//! Avalanche P2P protocol message definitions.
//!
//! This crate defines the protocol buffer messages used for peer-to-peer
//! communication in the Avalanche network. Messages are compatible with
//! the Go implementation's wire format.

pub mod p2p;

pub use p2p::*;

/// Operation types for P2P messages.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum Op {
    // Handshake messages
    Ping = 0,
    Pong = 1,
    Handshake = 2,
    GetPeerList = 3,
    PeerList = 4,

    // State sync messages
    GetStateSummaryFrontier = 10,
    StateSummaryFrontier = 11,
    GetAcceptedStateSummary = 12,
    AcceptedStateSummary = 13,

    // Bootstrapping messages
    GetAcceptedFrontier = 20,
    AcceptedFrontier = 21,
    GetAccepted = 22,
    Accepted = 23,
    GetAncestors = 24,
    Ancestors = 25,

    // Consensus messages
    Get = 30,
    Put = 31,
    PushQuery = 32,
    PullQuery = 33,
    Chits = 34,

    // App messages
    AppRequest = 40,
    AppResponse = 41,
    AppGossip = 42,
    AppError = 43,

    // Simplex messages
    Simplex = 50,

    // Internal (not sent over wire)
    Connected = 100,
    Disconnected = 101,
    Timeout = 102,
}

impl Op {
    /// Returns true if this operation requires a response.
    #[must_use]
    pub const fn expects_response(self) -> bool {
        matches!(
            self,
            Self::Ping
                | Self::GetPeerList
                | Self::GetStateSummaryFrontier
                | Self::GetAcceptedStateSummary
                | Self::GetAcceptedFrontier
                | Self::GetAccepted
                | Self::GetAncestors
                | Self::Get
                | Self::PushQuery
                | Self::PullQuery
                | Self::AppRequest
        )
    }

    /// Returns the response operation for this request operation.
    #[must_use]
    pub const fn response_op(self) -> Option<Self> {
        match self {
            Self::Ping => Some(Self::Pong),
            Self::GetPeerList => Some(Self::PeerList),
            Self::Handshake => Some(Self::PeerList),
            Self::GetStateSummaryFrontier => Some(Self::StateSummaryFrontier),
            Self::GetAcceptedStateSummary => Some(Self::AcceptedStateSummary),
            Self::GetAcceptedFrontier => Some(Self::AcceptedFrontier),
            Self::GetAccepted => Some(Self::Accepted),
            Self::GetAncestors => Some(Self::Ancestors),
            Self::Get => Some(Self::Put),
            Self::PushQuery | Self::PullQuery => Some(Self::Chits),
            Self::AppRequest => Some(Self::AppResponse),
            _ => None,
        }
    }

    /// Returns true if this message can be compressed.
    #[must_use]
    pub const fn compressible(self) -> bool {
        !matches!(self, Self::Ping | Self::Pong)
    }
}
