//! Validator set management.

use std::collections::HashMap;

use avalanche_ids::{Id, NodeId};
use parking_lot::RwLock;

use crate::{ConsensusError, Result};

/// A validator in the network.
#[derive(Debug, Clone)]
pub struct Validator {
    /// Node ID
    pub node_id: NodeId,
    /// Stake weight
    pub weight: u64,
    /// Public key (BLS)
    pub public_key: Option<Vec<u8>>,
}

/// Trait for validator set operations.
pub trait ValidatorSetTrait: Send + Sync {
    /// Returns the subnet ID.
    fn subnet_id(&self) -> Id;

    /// Gets a validator by node ID.
    fn get(&self, node_id: &NodeId) -> Option<Validator>;

    /// Returns true if the validator exists.
    fn contains(&self, node_id: &NodeId) -> bool;

    /// Returns the number of validators.
    fn len(&self) -> usize;

    /// Returns true if empty.
    fn is_empty(&self) -> bool;

    /// Returns the total stake weight.
    fn total_weight(&self) -> u64;

    /// Returns all validators.
    fn validators(&self) -> Vec<Validator>;

    /// Returns all node IDs.
    fn node_ids(&self) -> Vec<NodeId>;

    /// Samples k validators weighted by stake.
    fn sample(&self, k: usize) -> Result<Vec<NodeId>>;

    /// Gets the weight of a validator.
    fn weight(&self, node_id: &NodeId) -> u64;
}

impl Validator {
    /// Creates a new validator.
    pub fn new(node_id: NodeId, weight: u64) -> Self {
        Self {
            node_id,
            weight,
            public_key: None,
        }
    }

    /// Creates a validator with a public key.
    pub fn with_public_key(node_id: NodeId, weight: u64, public_key: Vec<u8>) -> Self {
        Self {
            node_id,
            weight,
            public_key: Some(public_key),
        }
    }
}

/// A set of validators with weights.
#[derive(Debug)]
pub struct ValidatorSet {
    /// Validators indexed by node ID
    validators: RwLock<HashMap<NodeId, Validator>>,
    /// Total stake weight
    total_weight: RwLock<u64>,
    /// Subnet this set belongs to
    subnet_id: Id,
}

impl ValidatorSet {
    /// Creates a new empty validator set.
    pub fn new(subnet_id: Id) -> Self {
        Self {
            validators: RwLock::new(HashMap::new()),
            total_weight: RwLock::new(0),
            subnet_id,
        }
    }

    /// Returns the subnet ID.
    pub fn subnet_id(&self) -> Id {
        self.subnet_id
    }

    /// Adds a validator to the set.
    pub fn add(&self, validator: Validator) -> Result<()> {
        let mut validators = self.validators.write();
        let mut total = self.total_weight.write();

        if validators.contains_key(&validator.node_id) {
            return Err(ConsensusError::Internal(format!(
                "validator {} already exists",
                validator.node_id
            )));
        }

        *total += validator.weight;
        validators.insert(validator.node_id.clone(), validator);
        Ok(())
    }

    /// Removes a validator from the set.
    pub fn remove(&self, node_id: &NodeId) -> Result<()> {
        let mut validators = self.validators.write();
        let mut total = self.total_weight.write();

        if let Some(validator) = validators.remove(node_id) {
            *total = total.saturating_sub(validator.weight);
            Ok(())
        } else {
            Err(ConsensusError::Internal(format!(
                "validator {} not found",
                node_id
            )))
        }
    }

    /// Gets a validator by node ID.
    pub fn get(&self, node_id: &NodeId) -> Option<Validator> {
        self.validators.read().get(node_id).cloned()
    }

    /// Returns true if the validator exists.
    pub fn contains(&self, node_id: &NodeId) -> bool {
        self.validators.read().contains_key(node_id)
    }

    /// Returns the number of validators.
    pub fn len(&self) -> usize {
        self.validators.read().len()
    }

    /// Returns true if empty.
    pub fn is_empty(&self) -> bool {
        self.validators.read().is_empty()
    }

    /// Returns the total stake weight.
    pub fn total_weight(&self) -> u64 {
        *self.total_weight.read()
    }

    /// Returns all validators.
    pub fn validators(&self) -> Vec<Validator> {
        self.validators.read().values().cloned().collect()
    }

    /// Returns all node IDs.
    pub fn node_ids(&self) -> Vec<NodeId> {
        self.validators.read().keys().cloned().collect()
    }

    /// Samples k validators weighted by stake.
    ///
    /// Returns an error if there aren't enough validators.
    pub fn sample(&self, k: usize) -> Result<Vec<NodeId>> {
        let validators = self.validators.read();

        if validators.len() < k {
            return Err(ConsensusError::InsufficientValidators {
                needed: k,
                have: validators.len(),
            });
        }

        // Simple weighted sampling using cumulative weights
        // In production, this should use a more sophisticated algorithm
        let total = *self.total_weight.read();
        if total == 0 {
            return Err(ConsensusError::InsufficientValidators {
                needed: k,
                have: 0,
            });
        }

        let mut sampled = Vec::with_capacity(k);
        let mut used = std::collections::HashSet::new();

        // Build cumulative weight distribution
        let mut cumulative: Vec<(NodeId, u64)> = Vec::new();
        let mut running = 0u64;
        for (node_id, validator) in validators.iter() {
            running += validator.weight;
            cumulative.push((node_id.clone(), running));
        }

        // Sample using simple random selection
        // Note: In production, use a proper RNG
        let mut rng_state = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as u64;

        while sampled.len() < k {
            // Simple LCG for deterministic testing
            rng_state = rng_state.wrapping_mul(6364136223846793005).wrapping_add(1);
            let target = rng_state % total;

            // Find validator at this weight
            for (node_id, cum_weight) in &cumulative {
                if target < *cum_weight && !used.contains(node_id) {
                    sampled.push(node_id.clone());
                    used.insert(node_id.clone());
                    break;
                }
            }

            // Prevent infinite loop if we can't find enough unique validators
            if sampled.len() < k && used.len() == validators.len() {
                // Allow duplicates if necessary (shouldn't happen with proper params)
                break;
            }
        }

        if sampled.len() < k {
            return Err(ConsensusError::InsufficientValidators {
                needed: k,
                have: sampled.len(),
            });
        }

        Ok(sampled)
    }

    /// Gets the weight of a validator.
    pub fn weight(&self, node_id: &NodeId) -> u64 {
        self.validators
            .read()
            .get(node_id)
            .map(|v| v.weight)
            .unwrap_or(0)
    }
}

impl ValidatorSetTrait for ValidatorSet {
    fn subnet_id(&self) -> Id {
        self.subnet_id
    }

    fn get(&self, node_id: &NodeId) -> Option<Validator> {
        ValidatorSet::get(self, node_id)
    }

    fn contains(&self, node_id: &NodeId) -> bool {
        ValidatorSet::contains(self, node_id)
    }

    fn len(&self) -> usize {
        ValidatorSet::len(self)
    }

    fn is_empty(&self) -> bool {
        ValidatorSet::is_empty(self)
    }

    fn total_weight(&self) -> u64 {
        ValidatorSet::total_weight(self)
    }

    fn validators(&self) -> Vec<Validator> {
        ValidatorSet::validators(self)
    }

    fn node_ids(&self) -> Vec<NodeId> {
        ValidatorSet::node_ids(self)
    }

    fn sample(&self, k: usize) -> Result<Vec<NodeId>> {
        ValidatorSet::sample(self, k)
    }

    fn weight(&self, node_id: &NodeId) -> u64 {
        ValidatorSet::weight(self, node_id)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_node_id(byte: u8) -> NodeId {
        NodeId::from_slice(&[byte; 20]).unwrap()
    }

    #[test]
    fn test_validator_set_basic() {
        let subnet = Id::from_slice(&[0; 32]).unwrap();
        let set = ValidatorSet::new(subnet);

        let v1 = Validator::new(make_node_id(1), 100);
        let v2 = Validator::new(make_node_id(2), 200);

        set.add(v1).unwrap();
        set.add(v2).unwrap();

        assert_eq!(set.len(), 2);
        assert_eq!(set.total_weight(), 300);
        assert!(set.contains(&make_node_id(1)));
        assert!(!set.contains(&make_node_id(3)));
    }

    #[test]
    fn test_validator_set_remove() {
        let subnet = Id::from_slice(&[0; 32]).unwrap();
        let set = ValidatorSet::new(subnet);

        let v1 = Validator::new(make_node_id(1), 100);
        set.add(v1).unwrap();

        assert_eq!(set.total_weight(), 100);

        set.remove(&make_node_id(1)).unwrap();
        assert_eq!(set.len(), 0);
        assert_eq!(set.total_weight(), 0);
    }

    #[test]
    fn test_validator_set_sample() {
        let subnet = Id::from_slice(&[0; 32]).unwrap();
        let set = ValidatorSet::new(subnet);

        // Add several validators
        for i in 0..10 {
            let v = Validator::new(make_node_id(i), 100);
            set.add(v).unwrap();
        }

        // Sample should work
        let sample = set.sample(5).unwrap();
        assert_eq!(sample.len(), 5);

        // Sampling too many should fail
        assert!(set.sample(20).is_err());
    }

    #[test]
    fn test_validator_set_duplicate() {
        let subnet = Id::from_slice(&[0; 32]).unwrap();
        let set = ValidatorSet::new(subnet);

        let v1 = Validator::new(make_node_id(1), 100);
        set.add(v1.clone()).unwrap();

        // Adding duplicate should fail
        assert!(set.add(v1).is_err());
    }
}
