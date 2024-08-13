// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use super::{Database as DatabaseService, Iter};
use crate::rpcdb::{
    database_server::Database, CloseRequest, CloseResponse, CompactRequest, CompactResponse,
    DeleteRequest, DeleteResponse, GetRequest, GetResponse, HasRequest, HasResponse,
    HealthCheckResponse, IteratorErrorRequest, IteratorErrorResponse, IteratorNextRequest,
    IteratorNextResponse, IteratorReleaseRequest, IteratorReleaseResponse,
    NewIteratorWithStartAndPrefixRequest, NewIteratorWithStartAndPrefixResponse, PutRequest,
    PutResponse, WriteBatchRequest, WriteBatchResponse,
};
use firewood::v2::api::BatchOp;

use tonic::{async_trait, Request, Response, Status};

#[async_trait]
impl Database for DatabaseService {
    async fn has(&self, _request: Request<HasRequest>) -> Result<Response<HasResponse>, Status> {
        todo!()
        // let key = request.into_inner().key;
        // let revision = self.latest().await.into_status_result()?;

        // let val = revision.val(key).await.into_status_result()?;

        // let response = HasResponse {
        //     has: val.is_some(),
        //     ..Default::default()
        // };

        // Ok(Response::new(response))
    }

    async fn get(&self, _request: Request<GetRequest>) -> Result<Response<GetResponse>, Status> {
        todo!()
        // let key = request.into_inner().key;
        // let revision = self.latest().await.into_status_result()?;

        // let value = revision
        //     .val(key)
        //     .await
        //     .into_status_result()?
        //     .map(|v| v.to_vec());

        // let Some(value) = value else {
        //     return Err(Status::not_found("key not found"));
        // };

        // let response = GetResponse {
        //     value,
        //     ..Default::default()
        // };

        // Ok(Response::new(response))
    }

    async fn put(&self, _request: Request<PutRequest>) -> Result<Response<PutResponse>, Status> {
        todo!()
        // let PutRequest { key, value } = request.into_inner();
        // let batch = BatchOp::Put { key, value };
        // let proposal = self.db.propose(vec![batch]).await.into_status_result()?;
        // let _ = proposal.commit().await.into_status_result()?;

        // Ok(Response::new(PutResponse::default()))
    }

    async fn delete(
        &self,
        _request: Request<DeleteRequest>,
    ) -> Result<Response<DeleteResponse>, Status> {
        todo!()
        // let DeleteRequest { key } = request.into_inner();
        // let batch = BatchOp::<_, Vec<u8>>::Delete { key };
        // let proposal = self.db.propose(vec![batch]).await.into_status_result()?;
        // let _ = proposal.commit().await.into_status_result()?;

        // Ok(Response::new(DeleteResponse::default()))
    }

    async fn compact(
        &self,
        _request: Request<CompactRequest>,
    ) -> Result<Response<CompactResponse>, Status> {
        Err(Status::unimplemented("compact not implemented"))
    }

    async fn close(
        &self,
        _request: Request<CloseRequest>,
    ) -> Result<Response<CloseResponse>, Status> {
        Err(Status::unimplemented("close not implemented"))
    }

    async fn health_check(
        &self,
        _request: Request<()>,
    ) -> Result<Response<HealthCheckResponse>, Status> {
        // TODO: why is the response a Vec<u8>?
        Ok(Response::new(HealthCheckResponse::default()))
    }

    async fn write_batch(
        &self,
        _request: Request<WriteBatchRequest>,
    ) -> Result<Response<WriteBatchResponse>, Status> {
        todo!()
        // let WriteBatchRequest { puts, deletes } = request.into_inner();
        // let batch = puts
        //     .into_iter()
        //     .map(from_put_request)
        //     .chain(deletes.into_iter().map(from_delete_request))
        //     .collect();
        // let proposal = self.db.propose(batch).await.into_status_result()?;
        // let _ = proposal.commit().await.into_status_result()?;

        // Ok(Response::new(WriteBatchResponse::default()))
    }

    async fn new_iterator_with_start_and_prefix(
        &self,
        request: Request<NewIteratorWithStartAndPrefixRequest>,
    ) -> Result<Response<NewIteratorWithStartAndPrefixResponse>, Status> {
        let NewIteratorWithStartAndPrefixRequest {
            start: _,
            prefix: _,
        } = request.into_inner();

        // TODO: create the actual iterator
        let id = {
            let mut iters = self.iterators.lock().await;
            iters.insert(Iter)
        };

        Ok(Response::new(NewIteratorWithStartAndPrefixResponse { id }))
    }

    async fn iterator_next(
        &self,
        _request: Request<IteratorNextRequest>,
    ) -> Result<Response<IteratorNextResponse>, Status> {
        Err(Status::unimplemented("iterator_next not implemented"))
    }

    async fn iterator_error(
        &self,
        _request: Request<IteratorErrorRequest>,
    ) -> Result<Response<IteratorErrorResponse>, Status> {
        Err(Status::unimplemented("iterator_error not implemented"))
    }

    async fn iterator_release(
        &self,
        request: Request<IteratorReleaseRequest>,
    ) -> Result<Response<IteratorReleaseResponse>, Status> {
        let IteratorReleaseRequest { id } = request.into_inner();

        {
            let mut iters = self.iterators.lock().await;
            iters.remove(id);
        }

        Ok(Response::new(IteratorReleaseResponse::default()))
    }
}

fn _from_put_request(request: PutRequest) -> BatchOp<Vec<u8>, Vec<u8>> {
    BatchOp::Put {
        key: request.key,
        value: request.value,
    }
}

fn _from_delete_request(request: DeleteRequest) -> BatchOp<Vec<u8>, Vec<u8>> {
    BatchOp::Delete { key: request.key }
}
