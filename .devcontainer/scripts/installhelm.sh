#! /bin/bash 
set -e
set -x

# Install helm 3.0
DESIRED_VERSION=release-3.0 # pin to a release tag
curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash

# add the stable chart repo
helm repo add stable https://kubernetes-charts.storage.googleapis.com/
helm repo update