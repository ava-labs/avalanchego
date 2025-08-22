// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use firewood::v2::api::{self, ArcDynDbView, HashKey};
use metrics::counter;

use crate::DatabaseHandle;

impl DatabaseHandle<'_> {
    pub(crate) fn get_root(&self, root: HashKey) -> Result<ArcDynDbView, api::Error> {
        let mut cache_miss = false;
        let view = self.cached_view.get_or_try_insert_with(root, |key| {
            cache_miss = true;
            self.db.view(HashKey::clone(key))
        })?;

        if cache_miss {
            counter!("firewood.ffi.cached_view.miss").increment(1);
        } else {
            counter!("firewood.ffi.cached_view.hit").increment(1);
        }

        Ok(view)
    }

    pub(crate) fn clear_cached_view(&self) {
        self.cached_view.clear();
    }
}
