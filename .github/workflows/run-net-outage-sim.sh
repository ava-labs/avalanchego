set -o pipefail
set -e

SUCCESS=1

# Polls AvalancheGo until it's healthy. When it is,
# sets SUCCESS to 0 and returns. If AvalancheGo
# doesn't become healthy within 3 hours, sets
# SUCCESS to 1 and returns.
wait_until_healthy () {
  # timeout: if after 3 hours it is not healthy, return
  stop=$(date -d "+ 3 hour" +%s)
  # store the response code here
  response=0
  # while the endpoint doesn't return 200
  while [ $response -ne 200 ]
  do
    echo "Checking if local node is healthy..."
    # Ignore error in case of ephemeral failure to hit node's API
    response=$(curl --write-out '%{http_code}' --silent --output /dev/null localhost:9650/ext/health)
    echo "got status code $response from health endpoint"
    # check that 3 hours haven't passed
    now=$(date +%s)
    if [ $now -ge $stop ];
    then 
      # timeout: exit
      SUCCESS=1
      return
    fi  
    # no timeout yet, wait 30s until retry
    sleep 30
  done
  # response returned 200, therefore exit
  echo "Node became healthy"
  SUCCESS=0
}

#remove any existing database files
echo "removing existing database files..."
rm /opt/mainnet-db-daily* 2>/dev/null || true # Do || true to ignore error if files dont exist yet
rm -rf /var/lib/avalanchego 2>/dev/null || true # Do || true to ignore error if files dont exist yet
echo "done existing database files"

#download latest mainnet DB backup
FILENAME="mainnet-db-daily-"
DATE=`date +'%m-%d-%Y'`
DB_FILE="$FILENAME$DATE"
echo "Copying database file $DB_FILE from S3 to local..."
aws s3 cp s3://avalanche-db-daily/ /opt/ --no-progress --recursive --exclude "*" --include "$DB_FILE*" 
echo "Done downloading database"

# extract DB
echo "Extracting database..."
mkdir -p /var/lib/avalanchego/db 
tar -zxf /opt/$DB_FILE*-tar.gz -C /var/lib/avalanchego/db 
echo "Done extracting database"

echo "Creating Docker network..."
docker network create controlled-net 

echo "Starting Docker container..."
containerID=$(docker run --name="net_outage_simulation" --memory="12g" --memory-reservation="11g" --cpus="6.0" --net=controlled-net -p 9650:9650 -p 9651:9651 -v /var/lib/avalanchego/db:/db -d avaplatform/avalanchego:latest /avalanchego/build/avalanchego --db-dir /db --http-host=0.0.0.0)

echo "Waiting 30 seconds for node to start..."
sleep 30
echo "Waiting until healthy..."
wait_until_healthy
if [ $SUCCESS -eq 1 ];
then
  echo "Timed out waiting for node to become healthy; exiting."
  exit 1 
fi

# To simulate internet outage, we will disable the docker network connection 
echo "Disconnecting node from internet..."
docker network disconnect controlled-net $containerID
echo "Sleeping 60 minutes..."
sleep 3600 
echo "Reconnecting node to internet..."
docker network connect controlled-net $containerID
echo "Reconnected to internet. Waiting until healthy..."

# now repeatedly check the node's health until it returns healthy
start=$(date +%s)
SUCCESS=-1
wait_until_healthy
if [ $SUCCESS -eq 1 ];
then
  echo "Timed out waiting for node to become healthy after outage; exiting."
  exit 1 
fi

# The node returned healthy, print how long it took
end=$(date +%s)

DELAY=$(($end - $start))
echo "Node became healthy again after complete outage after $DELAY seconds."
echo "Test completed"
