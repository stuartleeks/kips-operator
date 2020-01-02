# kips-sample-app

The `kips-sample-app` is a really simple sample app written in Go to give a walkthrough of using kips. It has a `web` UI that calls to the `api`:

```asciiart
+---------+     +------------+       +---------+     +------------+
|Service: +---->+Deployment: +------>+Service: +---->+Deployment: |
|web      |     |web         |       |api      |     |api         |
+---------+     +------------+       +---------+     +------------+
```

The `web` and `api` components are in their own folders and there is an additional `manifests` folder with the YAML for deploying them.

## Pre-requisites

This walkthrough assumes that you have

- deployed the kips-operator in your Kubernetes cluster
- have installed `azbridge` on your local machine
- an Azure Relay created
- have set the `AZBRIDGE_RELAY_CONNSTR` to the connection string for your Azure Relay

## Setting up the initial deployment

From a shell in the `manifests` folder, the api can be deployed via:

```bash
kubectl apply -f api
```

Similarly, the web component can be deployed via:

```bash
kubectl apply -f web
```

The web deployment creates a service of type `LoadBalancer` so after deploying run `kubectl get service web` to get the `EXTERNAL-IP`. You can test that everything is deployed and working by browsing to `http://your-ip:8080`. (Feel free to remove the load balancer line in the service and use port-forwarding instead)

![simple web page showing `WebValue` and `ApiValue` as `in-cluster`](images/sample-web.png)

The `WebValue` shown is an environment variable for the `web` component that is set to `in-cluster` by the deployment
The `ApiValue` come from calling the `api` component and the value comes from an environment variable set on the `api` componenet. There is another environment variable for the `web` component that specifies the URL for accessing the `api`

## One-way Redirection (running api locally)

The simplest scenario for `ServiceBridge` is to perform one-way redirection, in this example to redirect the in-cluster `api` Service to communicate with an `api` running locally. To do this we deploy a `ServiceBridge` that deploys `kips` (a container with `azbridge` listening) and updates the `api` service to target that deployment. We then run `azbridge` locally which connects to the in-cluster azbridge over Azure Relay.

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

### Run the API locally

From the `api` folder, run `API_VALUE=local go run ./main.go`. This will run the `api` locally and sets the value that it will return to `local` (rather than `in-cluster`). Verify that the api is running by accessing <http://localhost:9000>

### Deploy the ServiceBridge

With the api running locally the next step is to deploy the `ServiceBridge`

```bash
kubectl apply -f kips_v1alpha1_servicebridge-api.yaml
```

This deploys a `ServiceBridge` that targets the `api` service and sets the remote port (remote from the cluster perspective) to port 9000 (the port that the local api is running on) as shown below:

```yaml
apiVersion: kips.faux.ninja/v1alpha1
kind: ServiceBridge
metadata:
  name: servicebridge-sample-api
spec:
  targetService: # This is the service that we're redirecting and routing to the remote (dev) machine
    name: api
    ports:
    - name: api
      remotePort:  9000
  additionalServices: []
```

### Connect to the ServiceBridge

The `ServiceBridge` status contains the `azbridge` configuration required to connect to the in-cluster forwarder. The `scripts/azbridge-client.sh` script simplifies the steps of pulling down this config and starting `azbridge`.

```bash
./scripts/azbridge-client.sh --service-bridge servicebridge-sample-api
```

### Testing

At this point you should be able to access the `web` UI again (with the IP address from the initial deployment `http://your-ip:8080`) and it should show the ApiValue as `local`. You should also see log output from the locally running `api` showing that it has been accessed.

### Cleaning up

To revert to the in-cluster services, stop the local bridge and delete the `ServiceBridge`:

```bash
kubectl delete -f kips_v1alpha1_servicebridge-api.yaml
```

## Two-way Redirection (running web locally)

TODO