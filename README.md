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

TODO

* building azbridge image
* building operator image
* deploying operator (CRDs, operator, config/secrets)

## TODO

* Docs
  * installation
  * example of using (e.g. how to achieve each of the examples in the goals)
* installation/config
  * Need to be able to configure the image for azbridge when deploying the operator
  * Need to be able to configure the image for the operator when deploying
* Test multiple ports for a service
* Implement two-way redirection
* Script/util for running local side
  * set up host names?
  * grab config from servicebridge status
  * run azbridge
  * clean up host names?
  