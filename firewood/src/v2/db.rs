// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::db::DbError;
use crate::v2::api;

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
            DbError::Merkle(e) => api::Error::InternalError(Box::new(e)),
            DbError::IO(e) => api::Error::IO(e),
        }
    }
}
