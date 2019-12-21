#!/bin/bash

## TODO wait for running status

## TODO take arg
## TODO generate temp name
kubectl get servicebridges.kips.faux.ninja servicebridge-sample-api -o jsonpath="{.status.clientAzbridgeConfig}" > /tmp/azbridge-config.yaml

azbridge -f /tmp/azbridge-config.yaml -x $AZBRIDGE_RELAY_CONNSTR -v
