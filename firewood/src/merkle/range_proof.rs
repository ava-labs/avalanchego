// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::future::ready;

use futures::{StreamExt as _, TryStreamExt as _};

use crate::{
    shale::{disk_address::DiskAddress, ShaleStore},
    v2::api,
};

use super::{Merkle, Node};

impl<S: ShaleStore<Node> + Send + Sync> Merkle<S> {
    pub(crate) async fn range_proof<K: api::KeyType + Send + Sync>(
        &self,
        root: DiskAddress,
        first_key: Option<K>,
        last_key: Option<K>,
        limit: Option<usize>,
    ) -> Result<Option<api::RangeProof<Vec<u8>, Vec<u8>>>, api::Error> {
        // limit of 0 is always an empty RangeProof
        if let Some(0) = limit {
            return Ok(None);
        }

        let mut stream = self
            .get_iter(first_key, root)
            .map_err(|e| api::Error::InternalError(Box::new(e)))?;

        // fetch the first key from the stream
        let first_result = stream.next().await;

        // transpose the Option<Result<T, E>> to Result<Option<T>, E>
        // If this is an error, the ? operator will return it
        let Some((key, _)) = first_result.transpose()? else {
            // nothing returned, either the trie is empty or the key wasn't found
            return Ok(None);
        };

        let first_key_proof = self
            .prove(key, root)
            .map_err(|e| api::Error::InternalError(Box::new(e)))?;
        let limit = limit.map(|old_limit| old_limit - 1);

        // we stop streaming if either we hit the limit or the key returned was larger
        // than the largest key requested
        let mut middle = stream
            .take(limit.unwrap_or(usize::MAX))
            .take_while(|kv_result| {
                // no last key asked for, so keep going
                let Some(last_key) = last_key.as_ref() else {
                    return ready(true);
                };

                // return the error if there was one
                let Ok(kv) = kv_result else {
                    return ready(true);
                };

                // keep going if the key returned is less than the last key requested
                ready(kv.0.as_slice() <= last_key.as_ref())
            })
            .try_collect::<Vec<(Vec<u8>, Vec<u8>)>>()
            .await?;

        // remove the last key from middle and do a proof on it
        let last_key_proof = match middle.pop() {
            None => {
                return Ok(Some(api::RangeProof {
                    first_key_proof: first_key_proof.clone(),
                    middle: vec![],
                    last_key_proof: first_key_proof,
                }))
            }
            Some((last_key, _)) => self
                .prove(last_key, root)
                .map_err(|e| api::Error::InternalError(Box::new(e)))?,
        };

        Ok(Some(api::RangeProof {
            first_key_proof,
            middle,
            last_key_proof,
        }))
    }
}
