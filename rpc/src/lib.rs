pub mod sync {
    tonic::include_proto!("sync");
}

pub mod rpcdb {
    tonic::include_proto!("rpcdb");
}

pub mod service;

pub use service::Database as DatabaseService;
