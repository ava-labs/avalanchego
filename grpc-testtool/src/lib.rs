// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

#![doc = include_str!("../README.md")]

pub mod sync {
    #![expect(clippy::missing_const_for_fn)]
    tonic::include_proto!("sync");
}

pub mod rpcdb {
    #![expect(clippy::missing_const_for_fn)]
    tonic::include_proto!("rpcdb");
}

pub mod process_server {
    #![expect(clippy::missing_const_for_fn)]
    tonic::include_proto!("process");
}

pub mod service;

pub use service::Database as DatabaseService;
