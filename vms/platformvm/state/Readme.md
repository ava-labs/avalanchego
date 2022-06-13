# Platform VM database layout

In platform VM, database layout is as follows:

```
  VMDB  
  |-. validators  
  | |-. current  
  | | |-. validator  
  | | | '-. list  
  | | |   '-- txID -> uptime + potential reward  
  | | |-. delegator  
  | | | '-. list  
  | | |   '-- txID -> potential reward  
  | | '-. subnetValidator  
  | |   '-. list  
  | |     '-- txID -> nil  
  | |-. pending  
  | | |-. validator  
  | | | '-. list  
  | | |   '-- txID -> nil  
  | | |-. delegator  
  | | | '-. list  
  | | |   '-- txID -> nil  
  | | '-. subnetValidator  
  | |   '-. list  
  | |     '-- txID -> nil  
  | '-. diffs  
  |   '-. height+subnet  
  |     '-. list  
  |       '-- nodeID -> weightChange  
  |-. blocks  
  | '-- blockID -> block bytes  
  |-. txs  
  | '-- txID -> tx bytes + tx status  
  |- rewardUTXOs  
  | '-. txID  
  |   '-. list  
  |     '-- utxoID -> utxo bytes  
  |- utxos  
  | '-- utxoDB  
  |-. subnets  
  | '-. list  
  |   '-- txID -> nil  
  |-. chains  
  | '-. subnetID  
  |   '-. list  
  |     '-- txID -> nil  
  '-. singletons  
    |-- initializedKey -> nil  
    |-- timestampKey -> timestamp  
    |-- currentSupplyKey -> currentSupply  
    '-- lastAcceptedKey -> lastAccepted  
```
