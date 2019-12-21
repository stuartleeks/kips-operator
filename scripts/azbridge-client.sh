#!/bin/bash
set -e

BASE_DIR=$(dirname "$0")

SERVICE_BRIDGE=""

function show_usage() {
    echo "azbridge-client.sh"
    echo
    echo -e "\t--service-bridge\t(Required) Name of the service bridge to connect to"
}

while [[ $# -gt 0 ]]
do
    case "$1" in 
        --service-bridge)
            SERVICE_BRIDGE="$2"
            shift 2
            ;;
        *)
            echo "Unexpected '$1'"
            show_usage
            exit 1
            ;;
    esac
done

if [ -z $SERVICE_BRIDGE ]; then
    echo "service-bridge not specified"
    echo
    show_usage
    exit 1
fi

## TODO wait for running status

## TODO Support namespaces
## TODO generate temp name
kubectl get servicebridges.kips.faux.ninja $SERVICE_BRIDGE -o jsonpath="{.status.clientAzbridgeConfig}" > /tmp/azbridge-config.yaml

azbridge -f /tmp/azbridge-config.yaml -x $AZBRIDGE_RELAY_CONNSTR -v
