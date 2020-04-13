use std::collections::HashMap;
use lazy_static::lazy_static;
use std::sync::Mutex;

lazy_static! {
    pub static ref OWNERS: Mutex<HashMap<u32, Owner>> = Mutex::new(HashMap::new());
    pub static ref BAGS: Mutex<HashMap<u32, Bag>> = Mutex::new(HashMap::new());
}

pub enum Condition {
    New,
    Good,
    Bad,
    Destroyed
}

#[derive(Debug)]
pub struct Owner {
    pub id: u32,
    pub bags: Vec<u32> // Each element is the ID of a bag this owner owns
}

pub struct Bag {
    pub id: u32,
    pub owner_id: u32,
    pub condition: Condition,
    pub num_transfers: u32
}

// Create a new owner
// TODO: Error handling...what if owner with this ID already exists?
pub extern fn create_owner(id: u32) {
    OWNERS.lock().unwrap().insert(id, 
        Owner{
            id: id,
            bags: Vec::new()
        }
    );
}

// Create a new bag
// Precondition: There is an owner with the specified ID
// TODO error handling
pub extern fn create_bag(id: u32, owner_id: u32) {
    // Create the bag
    BAGS.lock().unwrap().insert(id, 
        Bag{
            id: id,
            owner_id: owner_id,
            condition: Condition::New,
            num_transfers: 0
        }
    );
    
    // Update the owner TODO this
    OWNERS.lock().unwrap().insert(id, 
        Bag{
            id: id,
            owner_id: owner_id,
            condition: Condition::New,
            num_transfers: 0
        }
    );
}

// Update the specified bag
pub extern update_bag(id: u32,)