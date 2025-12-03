// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE.md for licensing terms.

use crate::db::BatchOp;
use crate::merkle::{Key, Merkle, Value};
use crate::v2::api::IntoBatchIter;
use firewood_storage::logger::error;
use firewood_storage::{
    BranchNode, Child, Children, FileBacked, FileIoError, ImmutableProposal, LeafNode,
    MaybePersistedNode, MutableProposal, NibblesIterator, Node, NodeReader, NodeStore, Parentable,
    Path, PathComponent,
};
use rayon::ThreadPool;
use std::iter::once;
use std::ops::Deref;
use std::sync::mpsc::{Receiver, SendError, Sender};
use std::sync::{Arc, mpsc};

#[derive(Debug)]
struct WorkerSender(mpsc::Sender<BatchOp<Key, Value>>);

impl std::ops::Deref for WorkerSender {
    type Target = mpsc::Sender<BatchOp<Key, Value>>;

    fn deref(&self) -> &Self::Target {
        &self.0
    }
}

/// Response returned from a worker to the main thread. Includes the new root of the subtrie
/// at the given first path component and the deleted nodes.
#[derive(Debug)]
struct Response {
    first_path_component: PathComponent,
    root: Option<Child>,
    deleted_nodes: Vec<MaybePersistedNode>,
}

#[derive(Debug)]
pub enum CreateProposalError {
    FileIoError(FileIoError),
    SendError,
    InvalidConversionToPathComponent,
}

impl From<FileIoError> for CreateProposalError {
    fn from(err: FileIoError) -> Self {
        CreateProposalError::FileIoError(err)
    }
}

impl From<SendError<BatchOp<Key, Value>>> for CreateProposalError {
    fn from(_err: SendError<BatchOp<Key, Value>>) -> Self {
        CreateProposalError::SendError
    }
}

/// `ParallelMerkle` safely performs parallel modifications to a Merkle trie. It does this
/// by creating a worker for each subtrie from the root, and allowing the the workers to
/// perform inserts and removes to their subtries.
#[derive(Debug, Default)]
pub struct ParallelMerkle {
    workers: Children<Option<WorkerSender>>,
}

impl ParallelMerkle {
    /// Force the root (if necessary) into a branch with no partial path to allow the clean
    /// separation of the trie into an array of subtries that can be operated on independently
    /// by the worker threads.
    fn force_root(
        &self,
        proposal: &mut NodeStore<MutableProposal, FileBacked>,
    ) -> Result<Box<BranchNode>, CreateProposalError> {
        // There are 3 different cases to handle depending on the value of the root node.
        //
        // 1. If root is None, create a branch node with an empty partial path and a None for
        //    value. Create Nones for all of its children.
        // 2. If the existing root has a partial path, then create a new root with an empty
        //    partial path and a None for a value. Push down the previous root as a child. Note
        //    that this modified Merkle trie is not currently valid and may need to be updated
        //    during the post-processing step.
        // 3. If the existing root does not have a partial path, then there is nothing we need
        //    to do if it is a branch. If it is a leaf, then convert it into a branch.
        //
        // Cases 2 and 3 are handled by `force_branch_for_insert`. This function returns a branch
        // node with an empty partial path.
        proposal.root_mut().take().map_or_else(
            || {
                // Empty trie. Create a branch node with an empty partial path and a None for a value.
                Ok(BranchNode {
                    partial_path: Path::default(),
                    value: None,
                    children: Children::default(),
                }
                .into())
            },
            |node| {
                // Returns an error if it cannot convert a child index into a path component.
                node.force_branch_for_insert()
                    .map_err(|_| CreateProposalError::InvalidConversionToPathComponent)
            },
        )
    }

    /// After performing parallel modifications, it may be necessary to perform post processing to
    /// return the Merkle trie to the correct canonical form. This involves checking if the Merkle
    /// trie has an extra root node. If it does, apply a transform to return the trie to a valid
    /// state by following the steps below:
    ///
    /// If the root node has:
    /// 0 children and no value, the trie is empty. Just delete the root.
    /// 0 children and a value (from an empty key), the root should be a leaf
    /// 1 child and no value, the child should be the root (need to update partial path)
    /// In all other cases, the root is already correct.
    fn postprocess_trie(
        &self,
        nodestore: &mut NodeStore<MutableProposal, FileBacked>,
        mut branch: Box<BranchNode>,
    ) -> Result<Option<Node>, FileIoError> {
        let mut children_iter = branch
            .children
            .iter_mut()
            .filter_map(|(index, child)| child.as_mut().map(|child| (index, child)));

        let first_child = children_iter.next();
        match first_child {
            None => {
                Ok(branch.value.map(|value| {
                    // There is a value for the empty key. Create a leaf with the value and return.
                    Node::Leaf(LeafNode {
                        value,
                        partial_path: Path::default(), // Partial path should be empty
                    })
                }))
            }
            Some((child_index, child)) => {
                // Check if the root has a value or if there is more than one child. If yes, then
                // just return the root unmodified
                if branch.value.is_some() || children_iter.next().is_some() {
                    drop(children_iter);
                    return Ok(Some(Node::Branch(branch)));
                }

                // Return the child as the new root. Update its partial path to include the index value.
                let mut child = match child {
                    Child::Node(child_node) => std::mem::take(child_node),
                    Child::AddressWithHash(addr, _) => nodestore.read_for_update((*addr).into())?,
                    Child::MaybePersisted(maybe_persisted, _) => {
                        nodestore.read_for_update(maybe_persisted.clone())?
                    }
                };

                // The child's partial path is the concatenation of its (now removed) parent, which
                // should always be empty because of our prepare step, its (former) child index, and
                // its partial path. Because the parent's partial path should always be empty, we
                // can omit it and start with the `child_index`.
                let partial_path = Path::from_nibbles_iterator(
                    once(child_index.as_u8()).chain(child.partial_path().iter().copied()),
                );
                child.update_partial_path(partial_path);
                Ok(Some(child))
            }
        }
    }

    /// Call by a worker to processes requests from `child_receiver` and send back a response on
    /// `response_sender` once the main thread closes the child sender.
    fn worker_event_loop(
        mut merkle: Merkle<NodeStore<MutableProposal, FileBacked>>,
        first_path_component: PathComponent,
        child_receiver: Receiver<BatchOp<Key, Value>>,
        response_sender: Sender<Result<Response, FileIoError>>,
    ) -> Result<(), Box<SendError<Result<Response, FileIoError>>>> {
        // Wait for a message on the receiver child channel. Break out of loop when the sender has
        // closed the child sender.
        while let Ok(request) = child_receiver.recv() {
            if let Err(err) = match request {
                // insert a key-value pair into the subtrie
                BatchOp::Put { key, value } => {
                    let mut nibbles_iter = NibblesIterator::new(&key);
                    nibbles_iter.next(); // Skip the first nibble
                    merkle.insert_from_iter(nibbles_iter, value)
                }
                BatchOp::Delete { key } => {
                    let mut nibbles_iter = NibblesIterator::new(&key);
                    nibbles_iter.next(); // Skip the first nibble
                    merkle.remove_from_iter(nibbles_iter).map(|_| ())
                }
                BatchOp::DeleteRange { prefix } => {
                    let mut nibbles_iter = NibblesIterator::new(&prefix);
                    nibbles_iter.next(); // Skip the first nibble
                    merkle.remove_prefix_from_iter(nibbles_iter).map(|_| ())
                }
            } {
                response_sender.send(Err(err))?;
                break; // Stop handling additional requests
            }
        }
        // The main thread has closed the channel. Hash this subtrie and send back the worker's
        // response where the root is a Child::MaybePersisted.
        let mut nodestore = merkle.into_inner();
        let response = nodestore
            .root_mut()
            .take()
            .map(|root| {
                #[cfg(not(feature = "ethhash"))]
                let (root_node, root_hash) = NodeStore::<MutableProposal, FileBacked>::hash_helper(
                    root,
                    Path::from(&[first_path_component.as_u8()]),
                )?;
                #[cfg(feature = "ethhash")]
                let (root_node, root_hash) =
                    nodestore.hash_helper(root, Path::from(&[first_path_component.as_u8()]))?;
                Ok(Child::MaybePersisted(root_node, root_hash))
            })
            .transpose()
            .map(|hashed_root| Response {
                first_path_component,
                root: hashed_root,
                deleted_nodes: nodestore.take_deleted_nodes(),
            });
        response_sender.send(response)?;
        Ok(())
    }

    /// Creates a worker for performing operations on a subtrie, with the subtrie being determined
    /// by the value of the `first_path_component`.
    fn create_worker(
        pool: &ThreadPool,
        proposal: &NodeStore<MutableProposal, FileBacked>,
        root_branch: &mut BranchNode,
        first_path_component: PathComponent,
        response_sender: Sender<Result<Response, FileIoError>>,
    ) -> Result<WorkerSender, FileIoError> {
        // Create a channel for the coordinator (main thread) to send messages to this worker.
        let (child_sender, child_receiver) = mpsc::channel();

        // The root's child becomes the root node of the worker
        let child_root = root_branch
            .children
            .get_mut(first_path_component)
            .take()
            .map(|child| -> Result<_, FileIoError> {
                match child {
                    Child::Node(node) => Ok(node),
                    Child::AddressWithHash(address, _) => {
                        Ok(proposal.read_node(address)?.deref().clone())
                    }
                    Child::MaybePersisted(maybe_persisted, _) => {
                        Ok(maybe_persisted.as_shared_node(proposal)?.deref().clone())
                    }
                }
            })
            .transpose()?;

        // Build a nodestore from the child node
        let worker_nodestore = NodeStore::from_root(proposal, child_root);

        // Spawn a worker from the threadpool for this nibble. The worker will send messages to the coordinator
        // using `worker_sender`.
        pool.spawn(move || {
            if let Err(err) = ParallelMerkle::worker_event_loop(
                Merkle::from(worker_nodestore),
                first_path_component,
                child_receiver,
                response_sender,
            ) {
                error!("Worker cannot send to main thread using response channel: {err:?}");
            }
        });
        Ok(WorkerSender(child_sender))
    }

    // Collect responses from the workers, each representing the root of a subtrie and merge them into the
    // root node of the main trie.
    fn merge_children(
        &mut self,
        response_channel: Receiver<Result<Response, FileIoError>>,
        proposal: &mut NodeStore<MutableProposal, FileBacked>,
        root_branch: &mut BranchNode,
    ) -> Result<(), FileIoError> {
        while let Ok(response) = response_channel.recv() {
            match response {
                Ok(response) => {
                    // Adding deleted nodes (from calling read_for_update) from the child's nodestore.
                    proposal.delete_nodes(response.deleted_nodes.as_slice());

                    // Set the child at index to response.root which is the root of the child's subtrie.
                    *root_branch.children.get_mut(response.first_path_component) = response.root;
                }
                Err(err) => {
                    return Err(err); // Early termination.
                }
            }
        }
        Ok(())
    }

    /// Get a worker from the worker pool based on the `first_path_component` value. Create a worker if
    /// it doesn't exist already.
    fn worker(
        &mut self,
        pool: &ThreadPool,
        proposal: &NodeStore<MutableProposal, FileBacked>,
        root_branch: &mut BranchNode,
        first_path_component: PathComponent,
        response_sender: Sender<Result<Response, FileIoError>>,
    ) -> Result<&mut WorkerSender, FileIoError> {
        // Find the worker's state corresponding to the first nibble which is stored in an array.
        let worker_option = self.workers.get_mut(first_path_component);

        // Create a new worker if it doesn't exist. Not using `get_or_insert_with` with worker_option
        // because it is possible to generate a FileIoError within the closure.
        match worker_option {
            Some(worker) => Ok(worker),
            None => Ok(worker_option.insert(ParallelMerkle::create_worker(
                pool,
                proposal,
                root_branch,
                first_path_component,
                response_sender,
            )?)),
        }
    }

    /// Removes all of the entries in the trie. For the root entry, the value is removed but the
    /// root itself will remain. An empty root will only be removed during post processing.
    fn remove_all_entries(
        &self,
        root_branch: &mut BranchNode,
    ) -> Result<(), SendError<BatchOp<Key, Value>>> {
        for worker in self
            .workers
            .iter()
            .filter_map(|(_, worker)| worker.as_ref())
        {
            worker.send(BatchOp::DeleteRange {
                prefix: Box::default(), // Empty prefix
            })?;
        }
        // Also set the root value to None but does not delete the root.
        root_branch.value = None;
        Ok(())
    }

    /// The parent thread may receive a `SendError` if the worker that it is sending to has
    /// returned to the threadpool after encountering a `FileIoError`. This function should
    /// be called after receiving a `SendError` to find and propagate the `FileIoError`.
    fn find_fileio_error(
        response_receiver: &Receiver<Result<Response, FileIoError>>,
    ) -> Result<(), FileIoError> {
        // Go through the messages in the response channel without blocking to see if we can
        // find the FileIoError that caused the worker to close the channel, resulting in a
        // send error. If we can find it, then we propagate the FileIoError. Note that
        // successful responses can be in the response channel ahead of the FileIoError.
        // These are sent from workers that completed their requests without encountering
        // a FileIoError.
        for result in response_receiver.try_iter() {
            let _ = result?; // explicitly ignore the successful Response
        }
        Ok(())
    }

    /// Creates a parallel proposal in 4 steps: Prepare, Split, Merge, and Post-process. In the
    /// Prepare step, the trie is modified to ensure that the root is a branch node with no
    /// partial path. In the split step, entries from the batch are sent to workers that
    /// independently modify their sub-tries. In the merge step, the sub-tries are merged back
    /// to the main trie. Finally, in the post-processing step, the trie is returned to its
    /// canonical form.
    ///
    /// # Errors
    ///
    /// Returns a `CreateProposalError::FileIoError` if it encounters an error fetching nodes
    /// from storage, a `CreateProposalError::SendError` if it is unable to send messages to
    /// the workers, and a `CreateProposalError::InvalidConversionToPathComponent` if it is
    /// unable to convert a u8 index into a path component.
    pub fn create_proposal<T: Parentable>(
        &mut self,
        parent: &NodeStore<T, FileBacked>,
        batch: impl IntoBatchIter,
        pool: &ThreadPool,
    ) -> Result<Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>>, CreateProposalError> {
        // Create a mutable nodestore from the parent
        let mut mutable_nodestore = NodeStore::new(parent)?;

        // Prepare step: Force the root into a branch with no partial path in preparation for
        // performing parallel modifications to the trie.
        let mut root_branch = self.force_root(&mut mutable_nodestore)?;

        // Create a response channel the workers use to send messages back to the coordinator (us)
        let (response_sender, response_receiver) = mpsc::channel();

        // Split step: for each operation in the batch, send a request to the worker that is
        // responsible for the sub-trie corresponding to the operation's first nibble.
        for res in batch.into_batch_iter::<CreateProposalError>() {
            let op = res?;
            // Get the first nibble of the key to determine which worker to send the request to.
            //
            // Need to handle an empty key. Since the partial_path of the root must be empty, an
            // empty key should always be for the root node. There are 3 cases to consider.
            //
            // Insert: The main thread modifies the value of the root.
            //
            // Remove: The main thread removes any value at the root. However, it should not delete
            //         the root node, which, if necessary later, will be done in post processing.
            //
            // Remove Prefix:
            //         For a remove prefix, we would need to remove everything. We do this by sending
            //         a remove prefix with an empty prefix to all of the children, then removing the
            //         value of the root node.
            let mut key_nibbles = NibblesIterator::new(op.key().as_ref());
            let Some(first_path_component) = key_nibbles.next() else {
                match &op {
                    BatchOp::Put { key: _, value } => {
                        root_branch.value = Some(value.as_ref().into());
                    }
                    BatchOp::Delete { key: _ } => {
                        root_branch.value = None;
                    }
                    BatchOp::DeleteRange { prefix: _ } => {
                        // Calling remove prefix with an empty prefix is equivalent to a remove all.
                        if let Err(err) = self.remove_all_entries(&mut root_branch) {
                            // A send error is most likely due to a worker returning to the thread pool
                            // after it encountered a FileIoError. Try to find the FileIoError in the
                            // response channel and return that instead.
                            ParallelMerkle::find_fileio_error(&response_receiver)?;
                            return Err(err.into());
                        }
                    }
                }
                continue; // Done with this empty key operation.
            };

            // Verify that the worker index taken from the first nibble is valid.
            let first_path_component = PathComponent::try_new(first_path_component)
                .ok_or(CreateProposalError::InvalidConversionToPathComponent)?;

            // Get the worker that is responsible for this nibble. The worker will be created if it
            // doesn't already exist.
            let worker = self.worker(
                pool,
                &mutable_nodestore,
                &mut root_branch,
                first_path_component,
                response_sender.clone(),
            )?;

            // Send the current operation to the worker.
            // TODO: Currently the key from the BatchOp is copied to a Box<[u8]> before it is sent
            //       to the worker. It may be possible to send a nibble iterator instead of a
            //       Box<[u8]> to the worker if we use rayon scoped threads. This change would
            //       eliminate a memory copy but may require some code refactoring.
            if let Err(err) = match &op {
                BatchOp::Put { key: _, value } => worker.send(BatchOp::Put {
                    key: op.key().as_ref().into(),
                    value: value.as_ref().into(),
                }),
                BatchOp::Delete { key: _ } => worker.send(BatchOp::Delete {
                    key: op.key().as_ref().into(),
                }),
                BatchOp::DeleteRange { prefix: _ } => worker.send(BatchOp::DeleteRange {
                    prefix: op.key().as_ref().into(),
                }),
            } {
                // A send error is most likely due to a worker returning to the thread pool
                // after it encountered a FileIoError. Try to find the FileIoError in the
                // response channel and return that instead.
                ParallelMerkle::find_fileio_error(&response_receiver)?;
                return Err(err.into());
            }
        }

        // Drop the sender response channel from the parent thread.
        drop(response_sender);

        // Setting the workers to default will close the senders to the workers. This will cause the
        // workers to send back their responses.
        self.workers = Children::default();

        // Merge step: Collect the results from the workers and merge them as children to the root.
        self.merge_children(response_receiver, &mut mutable_nodestore, &mut root_branch)?;

        // Post-process step: return the trie to its canonical form.
        *mutable_nodestore.root_mut() =
            self.postprocess_trie(&mut mutable_nodestore, root_branch)?;

        let immutable: Arc<NodeStore<Arc<ImmutableProposal>, FileBacked>> =
            Arc::new(mutable_nodestore.try_into()?);

        Ok(immutable)
    }
}
