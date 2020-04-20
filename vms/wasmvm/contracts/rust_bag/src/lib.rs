use std::collections::HashMap;
use std::mem;
use lazy_static::lazy_static;
use std::sync::Mutex;

extern "C" {
    fn print(ptr: u32, len: u32);
    fn dbPut(key_ptr: u32, key_len: u32, value_ptr: u32, value_len: u32) -> u32;
    fn dbGet(key_ptr: u32, key_len: u32, value_ptr: u32) -> i32;
    fn dbGetValueLen(key_ptr: u32, key_len: u32) -> i32;
}

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
    pub price: u32,
    pub owner_id: u32,
    pub num_transfers: u32,
    pub condition: Condition
}

// Create a new owner
// Returns 0 on success
// Returns -1 otherwise
#[no_mangle]
pub extern fn create_owner(id: u32) -> i32 {
    let mut owners = OWNERS.lock().unwrap();
    match owners.get(&id) {
        Some(_) => -1,
        None => {
            owners.insert(id, Owner{
                id: id,
                bags: Vec::new()
            });
            0
        }
    }
}

// Create a new bag
// Precondition: There is an owner with the specified ID
// Returns 0 on success
// Returns -1 otherwise
#[no_mangle]
pub extern fn create_bag(id: u32, owner_id: u32, price: u32) -> i32 {
    // Check that the bag doesn't exist and the owner does 
    let mut bags = BAGS.lock().unwrap();
    if let Some(_) = bags.get(&id) {
        return -1
    }

    let owners = &mut OWNERS.lock().unwrap();
    if let None = owners.get(&owner_id) {
        return -1
    }

    // Update bag list
    bags.insert(id, Bag{
            id: id,
            owner_id: owner_id,
            num_transfers: 0,
            price: price,
            condition: Condition::New
        }
    );
    
    // Update the owner
    owners.get_mut(&owner_id).unwrap().bags.push(id);
    0 //success
}

// Update the specified bag's price
// Returns 0 on success
// Returns -1 if the bag doesn't exist
#[no_mangle]
pub extern fn update_bag_price(id: u32, price: u32) -> i32 {
    let bags = &mut BAGS.lock().unwrap();
    if let Some(bag) = bags.get_mut(&id) {
        bag.price=price;
        0
    } else {
        -1
    }
}

// Get a bag's price
// Returns -1 if the bag doesn't exist
#[no_mangle]
pub extern fn get_bag_price(id: u32) -> i32 {
    if let Some(bag) = BAGS.lock().unwrap().get(&id) {
        bag.price as i32
    } else {
        -1
    }
}

// Return the number of bags the specified owner owns
// Returns -1 if the owner doesn't exist
#[no_mangle]
pub extern fn get_num_bags(id: u32) -> i32 {
    if let Some(owner) = OWNERS.lock().unwrap().get(&id) {
        owner.bags.len() as i32
    } else {
        -1
    }
}

// Return the ID of the owner of the specified bag
// Returns -1 if the bag doesn't exist
#[no_mangle]
pub extern fn get_owner(id: u32) -> i32 {
    if let Some(bag) = BAGS.lock().unwrap().get(&id) {
        bag.owner_id as i32
    } else {
        -1
    }
}

// Transfer a bag to a new owner
// Returns -1 if the bag or new owner don't exist
#[no_mangle]
pub extern fn transfer_bag(id: u32, new_owner_id:u32) -> i32 {
    // Check that the bag and new owner exist 
    let mut bags = BAGS.lock().unwrap();
    let bag: &mut Bag;
    if let Some(_bag) = bags.get_mut(&id) {
      bag = _bag;
    } else {
        return -1 // bag doesn't exist
    }

    let owners = &mut OWNERS.lock().unwrap();
    let new_owner: &mut Owner;
    if let Some(owner) = owners.get_mut(&new_owner_id) {
        new_owner = owner;
    } else {
        return -1 // new owner doesn't exist
    }

    // Update the new owner
    new_owner.bags.push(bag.id);

    // Update the bag's current owner
    // get the index of the bag in the current owner's bag list
    let current_owner = owners.get_mut(&bag.owner_id).unwrap();
    let index = current_owner.bags.iter().position(|&r| r == id).unwrap();
    owners.get_mut(&bag.owner_id).unwrap().bags.remove(index);

    // Update the bag
    bag.owner_id = new_owner_id;    

    0 // success
}

// Prints "Hello, world!"
#[no_mangle]
pub extern fn say_hello() {
    let ptr = b"Hello, world!".as_ptr();
    unsafe {print(ptr as u32, 13);}
}

// Put KV pair "hello" -> "world" in the contract's DB
#[no_mangle]
pub extern fn put_hello() {
    let key_ptr = b"hello".as_ptr();
    let value_ptr = b"world".as_ptr();
    unsafe {dbPut(key_ptr as u32, 5, value_ptr as u32, 5);}
}

// Parse the byte args to this method, parse them to JSON, and print them
#[no_mangle]
pub extern fn parse_json() -> i32 {
    unsafe { 
        let args_len = dbGetValueLen(0, 0);
        if args_len == -1 { // couldn't get args len
            return -1
        }
        let mut buffer: std::vec::Vec<u8> = Vec::with_capacity(args_len as usize);
        let pointer = buffer.as_mut_ptr();
        let success = dbGet(0,0, pointer as u32);
        if success == -1 {
            return -1
        }
        print(pointer as u32, args_len as u32);
    }
    return 1;
}

/*
// Get value mapped to by "hello" in the contract's DB
#[no_mangle]
pub extern fn get_hello() {
    let keyPtr = b"hello".as_ptr();

    unsafe {
        let valueLen = dbGet(keyPtr: u32, keyLen: u32, valuePtr: u32)(ptr as u32, 5);
    }
}
*/