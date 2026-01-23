//! Avalanche consensus implementation (Snow family).
//!
//! This crate provides the Snowball and Snowman consensus protocols used by Avalanche.
//!
//! # Architecture
//!
//! - **Snowball**: Core binary consensus algorithm
//! - **Snowman**: Linear chain consensus built on Snowball
//! - **Engine**: State machine managing consensus lifecycle
//! - **Validators**: Validator set management and sampling
//! - **Mempool**: Transaction pool with priority ordering
//! - **Block Builder**: Block assembly from mempool
//! - **State Sync**: Fast state synchronization
//! - **Bootstrapper**: Chain bootstrapping
//!
//! # Example
//!
//! ```
//! use avalanche_snow::{Parameters, Snowball};
//!
//! let params = Parameters::default();
//! let mut snowball = Snowball::new(params);
//! ```

mod block_builder;
mod bootstrapper;
mod consensus;
mod engine;
mod error;
pub mod executor;
mod mempool;
mod parameters;
pub mod proposervm;
mod sync;
pub mod sync_client;
mod validators;

pub use block_builder::{BlockBuilder, BlockBuilderConfig, BlockProducer, BlockTx, BuiltBlock};
pub use bootstrapper::{BootstrapConfig, BootstrapPhase, Bootstrapper, FetchedBlock};
pub use consensus::{snowball::Snowball, snowman::Snowman, Consensus, Decidable};
pub use engine::{Engine, EngineState};
pub use error::{ConsensusError, Result};
pub use executor::{
    BlockAcceptor, BlockExecutor, BlockStatus, BlockVerifier, ExecutableBlock,
    ExecutionPipeline, ExecutorConfig, ExecutorEvent, PipelineStats,
};
pub use mempool::{AddResult, Mempool, MempoolConfig, MempoolTx, RejectReason, TxPriority};
pub use parameters::Parameters;
pub use sync::{StateChunk, StateSync, StateSyncConfig, StateSummary, SyncPhase};
pub use sync_client::{SyncClientConfig, SyncEngine, SyncNetwork, SyncResponse};
pub use validators::{Validator, ValidatorSet};
pub use proposervm::{
    ProposerVM, ProposerVMConfig,
    block::{PostForkBlock, PreForkBlock, ProposerBlock},
    scheduler::{ProposerScheduler, RoundRobinScheduler, StakeWeightedScheduler, WindowScheduler},
    state::{BlockMetadata, ProposerState, StateStats},
};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parameters_default() {
        let params = Parameters::default();
        assert!(params.k > 0);
        assert!(params.alpha > 0);
        assert!(params.alpha <= params.k);
    }
}
