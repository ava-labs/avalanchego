#!/bin/bash

set -ev

brew update
brew install docker
brew install docker-machine
brew cask install virtualbox
docker-machine create --driver virtualbox default
docker-machine start default
# hack to address problem with using DOCKER_BUILDKIT=1, inspired by:
# * https://github.com/rootless-containers/usernetes/blob/master/.travis.yml
#
# links discussing the issue:
# * https://github.com/moby/buildkit/issues/606#issuecomment-453959632
# * https://travis-ci.community/t/docker-builds-are-broken-if-buildkit-is-used-docker-buildkit-1/2994
# * https://github.com/moby/moby/issues/39120

# IS THIS HACK NEEDED FOR MAC?
#docker --version
#cat /etc/docker/daemon.json
#rm -f /etc/docker/daemon.json
#systemctl restart docker
