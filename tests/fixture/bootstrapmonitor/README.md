# bootstrap-monitor

Code rooted at this package implements a bootstrap-monitor binary
intended to enable continous bootstrap testing for avalanchego networks.

## Bootstrap testing

Bootstrapping an avalanchego node on a persistent network like mainnet
or fuji requires that the version of avalanchego that the node is
running be compatible with the historical data of that
network. Bootstrapping regularly is a good way of insuring against
regressions in compatibility.

## Types of bootstrap

### Full Sync

### Pruning

### State Sync

## Package details

## ArgoCD

- will need to ignore differences in the image tags of the statefulsets

## Alternatives considered

### Run bootstrap tests on hosted github workers

 - allow triggering / reporting to happen with github
   - but 5 day limit on job duration wouldn't probably wouldn't support full-sync

### Adding a 'bootstrap mode' to avalanchego
 - with a --bootstrap-mode flag, exit on successful bootstrap
   - but using it without a controller would require using `latest` to
     ensure that the node version could change on restarts
   - but when using `latest` there is no way to avoid having pod
     restart preventing the completion of an in-process bootstrap
     test. Only by using a specific image tag will it be possible for
     a restarted pod to reliably resume a bootstrap test.
