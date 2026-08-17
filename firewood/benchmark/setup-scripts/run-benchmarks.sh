DB="$HOME/firewood/benchmark-db"
# 10M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 -d "$DB"-10M create &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 -d "$DB"-10M zipf &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 1000 -d "$DB"-10M single &
wait

# 50M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 -d "$DB"-50M create &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 -d "$DB"-50M zipf &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 5000 -d "$DB"-50M single &
wait

# 100M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 -d "$DB"-100M create &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 -d "$DB"-100M zipf &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 10000 -d "$DB"-100M single &
wait

# 500M rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 50000 -d "$DB"-500M create &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 50000 -d "$DB"-500M zipf &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 50000 -d "$DB"-500M single &
wait

# 1B rows:
nohup time cargo run --profile maxperf --bin benchmark -- -n 100000 -d "$DB"-1B create &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 100000 -d "$DB"-1B zipf &
wait
nohup time cargo run --profile maxperf --bin benchmark -- -n 100000 -d "$DB"-1B single &
wait
