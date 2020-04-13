use std::collections::HashMap;
use lazy_static::lazy_static;
use std::sync::Mutex;

lazy_static! {
    pub static ref OWNERS: Mutex<HashMap<u32, Owner>> = Mutex::new(HashMap::new());
    static ref BAGS: HashMap<u32, Bag> = HashMap::new();
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
    pub condition: Condition
}
