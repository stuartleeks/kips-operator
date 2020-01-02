# kips-operator

This KIPS (Kubernetes In-cluster Pseudo-Service) operator.

Since the 'p' in 'pseudo' is silent, I wonder if this should be pronounced 'kiss'‽ ;-)

BE AWARE: This project is best considered as a sample. I created it as a pet project to explore [azure-relay-bridge](https://github.com/clemensv/azure-relay-bridge), which itself has a non-production disclaimer.

There are **many** approaches for doing similar things including those listed below. (Note that this list is not exhaustive, nor an endorsement of items!)

* Azure Dev Spaces - https://docs.microsoft.com/en-us/azure/dev-spaces/
* https://www.telepresence.io/
* https://ngrok.com/
* https://github.com/inlets/inlets

## Goals

### Starting Point

The starting point for the scenarios discussed below is a simple application deployed into a Kubernetes cluster as shown below.

```asciiart
+---------+     +------------+       +---------+     +------------+
|Service: +---->+Deployment: +------>+Service: +---->+Deployment: |
|web      |     |web         |       |api      |     |api         |
+---------+     +------------+       +---------+     +------------+
```

### One-way Redirection

With this application deployed, one goal of my exploration was to be able to redirect traffic from the cluster to my dev machine, e.g.

```asciiart
+---cluster-----------------------------------------------------------------+       +--------+
|                                                                           |       |        |
|   +---------+     +------------+       +---------+     +--------------+   |       |        |
|   |Service: +---->+Deployment: +------>+Service: +---->+Deployment:   |   |       | Azure  |
|   |web      |     |web         |       |apis     |     |kips          +---------->+ Relay  |
|   +---------+     +------------+       +---------+     |(azbridge -L) |   |       |        |
|                                                        |              |   |       |        |
|                                                        +--------------+   |       |        |
|                                                                           |       |        |
+---------------------------------------------------------------------------+       |        |
                                                                                    |        |
+---dev---------------------------------------------------------------------+       |        |
|                                                                           |       |        |
|                                    +-------------+    +-------------+     |       |        |
|                                    |             |    |             |     |       |        |
|                                    | api server  +<---+azbridge -R  +------------>+        |
|                                    |             |    |             |     |       |        |
|                                    +-------------+    +-------------+     |       |        |
|                                                                           |       |        |
+---------------------------------------------------------------------------+       +--------+
```

### Two-way Redirection

In the first goal traffic was only routed from the cluster to a remote machine (my dev machine in this case). The second case I wanted to explore was redirected traffic both ways. In the example below, traffic to the web service is redirected to the web app running on my remote (dev) machine, but traffic from the remote (dev) machine to the api is routed back to the cluster.

```asciiart
+---cluster-----------------------------------------------------------------+       +--------+
|                                                                           |       |        |
|   +---------+     +------------+                                          |       |        |
|   |Service: +---->+Deployment: +<------------------------------------------------>+ Azure  |
|   |web      |     |azbridge    |       +---------+     +--------------+   |       | Relay  |
|   +---------+     +-L & -R     |       |Service: |     |Deployment:   |   |       |        |
|                   |            +------>+apis     +---->+api           |   |       |        |
|                   +------------+       +---------+     +--------------+   |       |        |
|                                                                           |       |        |
+---------------------------------------------------------------------------+       |        |
                                                                                    |        |
+---dev---------------------------------------------------------------------+       |        |
|                                                                           |       |        |
|                                    +-------------+    +-------------+     |       |        |
|                                    |             |    |             |     |       |        |
|                                    | web server  +<-->+azbridge     +<----------->+        |
|                                    |             |    +-L & -R      |     |       |        |
|                                    +-------------+    +-------------+     |       |        |
|                                                                           |       |        |
+---------------------------------------------------------------------------+       +--------+
```

## Installing

### Creating an Azure Relay

The `ServiceBridge` builds on `azbridge` which uses [Azure Relay](https://docs.microsoft.com/en-us/azure/service-bus-relay/relay-what-is-it) for connectivity.

After creating an Azure Relay, go to the "Shared access policies" section and grab the "Connection String".

Create a `AZBRIDGE_RELAY_CONNSTR` environment variable with the connection string for use by the client script:

```bash
export AZBRIDGE_RELAY_CONNSTR=Endpoint="sb://examplerelay.servicebus.windows.net/;SharedAccessKeyName=YourKeyName;SharedAccessKey=***MASKED***"
```

Next, create the secret for use by the operator:

```bash
kubectl create secret generic azbridge-connection-string --from-literal=connectionString=$AZBRIDGE_RELAY_CONNSTR
```

### Deploying the operator into the cluster

* deploying operator (CRDs, operator, config/secrets)

TODO

### Running the operator locally

TODO

### Setting up azbridge

You will need `azbridge` installed locally to connect to the `ServiceBridge`. See <https://github.com/Azure/azure-relay-bridge#downloads> for installation instructions.

## Getting started

See the [kips-sample-app Walkthrough](samples/kips-sample-app/README.md) for a guide to getting started with kips-operator.
