#!/usr/bin/env bash

set -ev

curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable"
sudo apt-get update
sudo apt-get -y -o Dpkg::Options::="--force-confnew" install docker-ce
# hack to address problem with using DOCKER_BUILDKIT=1, inspired by:
# * https://github.com/rootless-containers/usernetes/blob/master/.travis.yml
#
# links discussing the issue:
# * https://github.com/moby/buildkit/issues/606#issuecomment-453959632
# * https://travis-ci.community/t/docker-builds-are-broken-if-buildkit-is-used-docker-buildkit-1/2994
# * https://github.com/moby/moby/issues/39120
sudo docker --version
sudo cat /etc/docker/daemon.json
sudo rm -f /etc/docker/daemon.json
sudo systemctl restart docker
