#! /bin/bash
set -e

TIMESTAMP=$(date "+%Y%m%d-%H%M%S")

KIND_CLUSTER_NAME="kips-cluster"
KIND_NODE_VERSION=v1.15.3

IMG=docker.io/kips-operator:$TIMESTAMP # Use docker.io for loading into kind

header() {
    echo 
    echo "*"
    echo "* $1"
    echo "*"
}


header "Reset kind cluster"
CLUSTER_EXISTS=$(kind get clusters | grep -q ${KIND_CLUSTER_NAME}; echo $?) 
if [ "$CLUSTER_EXISTS" == "0" ]; then\
    kind delete cluster --name ${KIND_CLUSTER_NAME}; \
fi
kind create cluster --name ${KIND_CLUSTER_NAME} --image=kindest/node:${KIND_NODE_VERSION}

header "Connect to cluster"

export KUBECONFIG="$(kind get kubeconfig-path --name="${KIND_CLUSTER_NAME}")"
kubectl get nodes

header "Build and load image"
IMG=$IMG make kind-load-img

header "Installing CRDs"
IMG=$IMG make install

header "Installing operator"
IMG=$IMG make deploy

header "Run ginkgo tests"
ginkgo