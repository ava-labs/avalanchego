// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::{db::DbError, v2::api};

#[cfg_attr(doc, aquamarine::aquamarine)]
/// ```mermaid
/// graph LR
///     RevRootHash --> DBRevID
///     RevHeight --> DBRevID
///     DBRevID -- Identify --> DbRev
///     Db/Proposal -- propose with batch --> Proposal
///     Proposal -- translate --> DbRev
///     DB -- commit proposal --> DB
/// ```

impl From<DbError> for api::Error {
    fn from(value: DbError) -> Self {
        match value {
            DbError::InvalidParams => api::Error::InternalError(Box::new(value)),
            DbError::Merkle(e) => api::Error::InternalError(Box::new(e)),
            DbError::System(e) => api::Error::IO(e.into()),
            DbError::KeyNotFound | DbError::CreateError => {
                api::Error::InternalError(Box::new(value))
            }
            DbError::Shale(e) => api::Error::InternalError(Box::new(e)),
            DbError::IO(e) => api::Error::IO(e),
            DbError::InvalidProposal => api::Error::InvalidProposal,
        }
    }
}
