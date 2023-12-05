// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use rpc::{
    rpcdb::database_server::DatabaseServer as RpcServer, sync::db_server::DbServer as SyncServer,
    DatabaseService,
};
use std::sync::Arc;
use tonic::transport::Server;

// TODO: use clap to parse command line input to run the server
#[tokio::main]
async fn main() -> Result<(), tonic::transport::Error> {
    #[allow(clippy::unwrap_used)]
    let addr = "[::1]:10000".parse().unwrap();

    println!("Database-Server listening on: {}", addr);

    let svc = Arc::new(DatabaseService::default());

    // TODO: graceful shutdown
    Server::builder()
        .add_service(RpcServer::from_arc(svc.clone()))
        .add_service(SyncServer::from_arc(svc))
        .serve(addr)
        .await
}
