# Bootstrap testing

Bootstrapping an avalanchego node on a persistent network like mainnet
or fuji requires that the version of avalanchego that the node is
running be compatible with the historical data of that
network. Running this test regularly is a good way of insuring against
regressions in compatibility.


### Full Sync

### Pruning

### State Sync

## Architecture

### Controller

 - watches for deployments that manage bootstrap testing
 - if such a deployment is using `latest` image tags
   - get the version currently associated with `latest` (by running a pod)
   - update the image tags for the pod, which will prompt a restart to use the new tag
 - if such a deployment is using specific tags
   - check its health
     - require a service with the same name as the deployment
     - use an informer with a resync every 5 minutes?
       - i.e. only check health every 5 minutes
   - if healthy, get the latest version (by running a pod)
   - if the version is not different from the running version, do nothing
   - if the version is different, update the tags for the deploymnet
     - this will prompt a redeployment
 - maybe cache the version to avoid running too many pods?
   - actually, the check for a new version should be cheap. So every 5 minutes should be fine.

### Monitor

 - every [interval]
   - if node running on localhost port is healthy
     - get the image digest of the image that the node is running as
     - get image digest avalanchego image currently tagged `latest`
     - if the 2 digests differ
       - trigger a new bootstrap test by updating the image  for the deployment managing the pod

 - every [interval]
   - uses curl to check whether an avalanche node running on localhost is healthy
   - if the node is healthy
     - use kubectl to wait for a pod using image avaplatform/avalanchego:latest to complete
     - use kubectl to retrieve the image digest from the terminated pod
     - compose the expected image name using the image digest i.e. `avaplatform/avalanchego:[image digest of terminated pod]`
     - use kubectl to retrieve the image for the 'node' container in the same pod as the bash script
     - if the image for the node container does not match the expected image name
       - use kubectl to discover the name of the deployment that is managing the pod the script is running in
       - update the 'node' container of the deployment to use the expected image name


### Bootstrap pods

### ArgoCD

- will need to ignore differences to the image tags of the deployments

#### Containers

 - init container
   - uses avalanchego image
   - mounts /data
   - initializes the /data path by checking the version against one that was saved
```bash
version_path="/data/bootstrap_version.json"

latest_version=$(/avalanchego/build/avalanchego --version-json)

if [ -f "${version_path}" ] && diff <(echo "${latest_version}") "${version_path}"; then
  echo "Resuming bootstrap for ${latest_version}"
  exit 0
fi

echo "Starting bootstrap for ${latest_version}"

echo "Recording version"
echo "${latest_version}" > "${version_path}"

echo "Clearing Recording version"
rm -rf /data/node/*

# Ensure the node path exists
mkdir /data/node
```
 - avalanche container


## Alternatives considered

#### self-hosteed github workers

 - allow triggering / reporting to happen with github
   - but 5 day limit on job duration wouldn't probably wouldn't support full-sync

#### Adding a 'bootstrap mode' to avalanchego
 - with a --bootstrap-mode flag, exit on successful bootstrap
   - but using it without a controller would require using `latest` to
     ensure that the node version could change on restarts
   - but when using `latest` there is no way to avoid having pod
     restart preventing the completion of an in-process bootstrap
     test. Only by using a specific image tag will it be possible for
     a restarted pod to reliably resume a bootstrap test.


### Primary Requirement

 - Run full sync and state sync bootstrap tests against mainnet and testnet

### Secondary requiremnts

 - Run tests in infra-managed kubernetes
   - Ensures sufficient resources (~2tb required per test)
   - Ensures metrics and logs will be collected by Datadog
 - Ensure that no more than one test will be run against a given image
 - Ensure that an in-process bootstrap test can be resumed if a pod is restarted

### TODO
 - accepts bootstrap configurations
   - network-id
   - sync-mode
 - kube configuration
   - in-cluster or not
 - start a wait loop (via kubernetes)
   - check image version
   - for each bootstrap config
     - if config.running -
       - continue
     - if config.last_version !=  version
       - start a new test run
       - pass a


StartJob(clientset, namespace, imageName, pvcSize, flags)
  - starts node
  - periodically checks if node pod is running and node is healthy
  - errors out on timeout
    - log result (use zap)
  - on healthy
    - log result
    - update last version
    - update the config to '
