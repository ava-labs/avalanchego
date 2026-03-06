// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use std::fmt;

use firewood::merkle;
use firewood::v2::api::{self, ArcDynDbView, BoxKeyValueIter};
use firewood_metrics::MetricsContext;
use std::iter::FusedIterator;

type KeyValueItem = (merkle::Key, merkle::Value);

/// An opaque wrapper around a [`BoxKeyValueIter`] and a reference
/// to the [`ArcDynDbView`] backing it, preventing the view from
/// being dropped while iteration is in progress.
#[derive(Default)]
pub struct IteratorHandle<'view> {
    iter: Option<BoxKeyValueIter<'view>>,
    view: Option<ArcDynDbView>,
    metrics_context: Option<MetricsContext>,
}

impl fmt::Debug for IteratorHandle<'_> {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("IteratorHandle")
            .field("metrics_context", &self.metrics_context)
            .finish_non_exhaustive()
    }
}

impl<'view> IteratorHandle<'view> {
    #[must_use]
    pub(crate) fn new(
        view: ArcDynDbView,
        iter: BoxKeyValueIter<'view>,
        metrics_context: Option<MetricsContext>,
    ) -> Self {
        Self {
            iter: Some(iter),
            view: Some(view),
            metrics_context,
        }
    }
}

impl Iterator for IteratorHandle<'_> {
    type Item = Result<KeyValueItem, api::Error>;

    fn next(&mut self) -> Option<Self::Item> {
        let out = self.iter.as_mut()?.next();
        if out.is_none() {
            // iterator exhausted; drop it so the NodeStore can be released
            self.iter = None;
            self.view = None;
        }
        out.map(|res| res.map_err(api::Error::from))
    }
}

impl FusedIterator for IteratorHandle<'_> {}

#[expect(clippy::missing_errors_doc)]
impl IteratorHandle<'_> {
    pub fn iter_next_n(&mut self, n: usize) -> Result<Vec<KeyValueItem>, api::Error> {
        self.by_ref().take(n).collect()
    }
}

#[derive(Debug, Default)]
pub struct CreateIteratorResult<'db>(pub IteratorHandle<'db>);

impl crate::MetricsContextExt for IteratorHandle<'_> {
    fn metrics_context(&self) -> Option<MetricsContext> {
        self.metrics_context
    }
}
