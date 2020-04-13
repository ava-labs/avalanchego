use bag;

fn main() {
    let owner = bag::Owner{
        id:1,
        bags: Vec::new()
    };
    bag::OWNERS.lock().unwrap().insert(owner.id, owner);
    //bag::OWNERS.lock().unwrap().get(&owner.id);
    println!("bag: {:?}", bag::OWNERS.lock().unwrap().get(&1))
}