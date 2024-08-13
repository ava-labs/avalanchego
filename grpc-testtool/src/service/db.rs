// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::Database;
use crate::sync::{
    db_server::Db as DbServerTrait, CommitChangeProofRequest, CommitRangeProofRequest,
    GetChangeProofRequest, GetChangeProofResponse, GetMerkleRootResponse, GetProofRequest,
    GetProofResponse, GetRangeProofRequest, GetRangeProofResponse, VerifyChangeProofRequest,
    VerifyChangeProofResponse,
};
use tonic::{async_trait, Request, Response, Status};

#[async_trait]
impl DbServerTrait for Database {
    #[tracing::instrument(level = "trace")]
    async fn get_merkle_root(
        &self,
        _request: Request<()>,
    ) -> Result<Response<GetMerkleRootResponse>, Status> {
        todo!()
        // let root_hash = self.db.root_hash().await.into_status_result()?.to_vec();
        //
        // let response = GetMerkleRootResponse { root_hash };
        //
        // Ok(Response::new(response))
    }

    #[tracing::instrument(level = "trace")]
    async fn get_proof(
        &self,
        _request: Request<GetProofRequest>,
    ) -> Result<Response<GetProofResponse>, Status> {
        todo!()
        // let GetProofRequest { key: _ } = request.into_inner();
        // let _revision = self.latest().await.into_status_result()?;
    }

    #[tracing::instrument(level = "trace")]
    async fn get_change_proof(
        &self,
        _request: Request<GetChangeProofRequest>,
    ) -> Result<Response<GetChangeProofResponse>, Status> {
        todo!()
        // let GetChangeProofRequest {
        //     start_root_hash: _,
        //     end_root_hash: _,
        //     start_key: _,
        //     end_key: _,
        //     key_limit: _,
        // } = request.into_inner();

        // let _revision = self.latest().await.into_status_result()?;
    }

    #[tracing::instrument(level = "trace")]
    async fn verify_change_proof(
        &self,
        _request: Request<VerifyChangeProofRequest>,
    ) -> Result<Response<VerifyChangeProofResponse>, Status> {
        todo!()
        // let VerifyChangeProofRequest {
        //     proof: _,
        //     start_key: _,
        //     end_key: _,
        //     expected_root_hash: _,
        // } = request.into_inner();

        // let _revision = self.latest().await.into_status_result()?;
    }

    #[tracing::instrument(level = "trace")]
    async fn commit_change_proof(
        &self,
        _request: Request<CommitChangeProofRequest>,
    ) -> Result<Response<()>, Status> {
        todo!()
        //        let CommitChangeProofRequest { proof: _ } = request.into_inner();
    }

    #[tracing::instrument(level = "trace")]
    async fn get_range_proof(
        &self,
        request: Request<GetRangeProofRequest>,
    ) -> Result<Response<GetRangeProofResponse>, Status> {
        let GetRangeProofRequest {
            root_hash: _,
            start_key: _,
            end_key: _,
            key_limit: _,
        } = request.into_inner();

        todo!()
    }

    #[tracing::instrument(level = "trace")]
    async fn commit_range_proof(
        &self,
        request: Request<CommitRangeProofRequest>,
    ) -> Result<Response<()>, Status> {
        let CommitRangeProofRequest {
            start_key: _,
            range_proof: _,
        } = request.into_inner();

        todo!()
    }
}
