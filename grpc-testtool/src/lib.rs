// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

pub mod sync {
    #![allow(clippy::unwrap_used)]
    #![allow(clippy::missing_const_for_fn)]
    tonic::include_proto!("sync");
}

pub mod rpcdb {
    #![allow(clippy::unwrap_used)]
    #![allow(clippy::missing_const_for_fn)]
    tonic::include_proto!("rpcdb");
}

pub mod service;

pub use service::Database as DatabaseService;
