use tonic::{Request, Response, Status, async_trait};

use crate::process_server::process_server_service_server::ProcessServerService as ProcessTrait;

use crate::process_server::MetricsResponse;

#[async_trait]
impl ProcessTrait for super::Database {
    async fn metrics(&self, _request: Request<()>) -> Result<Response<MetricsResponse>, Status> {
        Err(Status::unimplemented("Metrics not yet supported"))
        // TODO: collect the metrics here
        // Ok(Response::new(MetricsResponse::default()))
    }
}
