use std::{path::Path, thread};

use crossbeam_channel::Sender;
use firewood::db::{DBConfig, DBError, DB};
use tokio::sync::oneshot;

type Action = Box<dyn FnOnce(&DB) + Send + 'static>;

/// Firewood connection manager communicates with the firewood database in a
/// separate thread through channels.
#[derive(Clone, Debug)]
pub struct Manager {
    // Channel used by the caller to communicate with firewood.
    action_tx: Sender<Action>,
}

impl Manager {
    pub async fn new<P: AsRef<Path>>(db_path: P, cfg: DBConfig) -> Result<Self, DBError> {
        let owned_path = db_path.as_ref().to_path_buf();
        initialize(move || DB::new(&owned_path.to_string_lossy(), &cfg)).await
    }

    /// Calls a db action over channel.
    // TODO: Remove static lifetime bounds
    pub async fn call<F, T>(&self, func: F) -> T
    where
        F: FnOnce(&DB) -> T + Send + 'static,
        T: Send + 'static,
    {
        // communicate result of action.
        let (result_tx, result_rx) = oneshot::channel();

        // sends an action function to the db via channel and returns the response.
        self.action_tx
            .send(Box::new(move |db| {
                let resp = func(db);
                let _ = result_tx.send(resp);
            }))
            .expect("action tx channel failed");

        result_rx.await.expect("action rx channel failed")
    }
}

/// Creates a new work thread which listens for actions over channel.
async fn initialize<I>(new_db: I) -> Result<Manager, DBError>
where
    I: FnOnce() -> Result<DB, DBError> + Send + 'static,
{
    // provides communication from the caller to the db in the work thread.
    let (action_tx, action_rx) = crossbeam_channel::unbounded::<Action>();
    // communicates the result of the db initialize action.
    let (init_tx, init_rx) = oneshot::channel();

    log::debug!("work thread spawned");
    thread::spawn(move || {
        let db = match new_db() {
            Ok(db) => db,
            Err(e) => {
                // return error
                let _ = init_tx.send(Err(e));
                return;
            }
        };

        if init_tx.send(Ok(())).is_err() {
            log::error!("failed to send db action result: ok");
            return;
        }

        // listen for actions
        while let Ok(func) = action_rx.recv() {
            log::debug!("action received");
            func(&db);
            log::debug!("action complete");
        }
    });

    init_rx
        .await
        .expect("result rx channel failed")
        .map(|_| Manager { action_tx })
}
