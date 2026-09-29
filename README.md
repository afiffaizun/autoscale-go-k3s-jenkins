# autoscale-go-k3s-jenkins

A learning project that runs a Go HTTP application on a **K3s** cluster with
**automated CI/CD via Jenkins**, **image scanning with Trivy**, and
**KEDA-based autoscaling**. Tailscale Funnel is used to expose access to the
internet.

## Architecture

```
                       ┌──────────────────────────────┐
                       │           GitHub             │
                       │  (source + webhook trigger)  │
                       └──────────────┬───────────────┘
                                      │ webhook (via Tailscale Funnel)
                                      ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                                Jenkins                                   │
│  Checkout → Go Test → Docker Build → Trivy Scan → Push → Deploy → Check │
└───────────┬───────────────────────────────────────────────┬──────────────┘
            │ docker push                                   │ kubectl apply / set image
            ▼                                               ▼
┌───────────────────────┐                 ┌─────────────────────────────────┐
│      Docker Hub       │                 │         K3s cluster             │
│ mafifdev/autoscale-   │  pull image     │  namespace: go-cicd             │
│       go-k3s          │ ──────────────► │  ├── Deployment  (go-app)       │
└───────────────────────┘                 │  ├── Service     (NodePort)     │
                                          │  └── KEDA ScaledObject          │
                                          └───────────────┬─────────────────┘
                                                          │ NodePort :31048
                                                          ▼
                                               http://<node-ip>:31048/health
```

- **Tailscale Funnel** is used generally to provide access over the internet
  (e.g. for the Jenkins webhook) without exposing ports manually.

## Tech Stack

| Layer       | Technology                                     |
| ----------- | ---------------------------------------------- |
| Language    | Go 1.25+ (built with `golang:1.26-alpine`)     |
| Packaging   | Docker multi-stage build (runtime: Alpine 3.22) |
| CI/CD       | Jenkins (declarative pipeline)                 |
| Security    | Trivy image scan (HIGH/CRITICAL gate)          |
| Registry    | Docker Hub (`mafifdev/autoscale-go-k3s`)       |
| Orchestration | K3s (Kubernetes)                             |
| Autoscaling | KEDA (`ScaledObject`, CPU-based)               |
| Networking  | Tailscale Funnel, NodePort Service             |
| Monitoring  | kube-prometheus-stack (Prometheus, Grafana, kube-state-metrics, node-exporter) |

## Project Structure

```
.
├── app/
│   ├── main.go            # HTTP server (/, /health, /cpu)
│   └── main_test.go       # Unit tests
├── k8s/
│   ├── namespace.yaml     # Namespace go-cicd
│   ├── deployment.yaml    # go-app Deployment (1 replica)
│   ├── service.yaml       # NodePort Service (80 -> 8080)
│   └── keda.yaml          # KEDA ScaledObject (1-5 replicas)
├── Dockerfile             # Multi-stage build
├── Jenkinsfile            # CI/CD pipeline definition
├── go.mod
└── .dockerignore
```

## API Endpoints

| Method | Path      | Description                                             |
| ------ | --------- | ------------------------------------------------------- |
| GET    | `/`       | Returns `Hello from Go CI/CD v3`                        |
| GET    | `/health` | Health check, returns `200 OK`                          |
| GET    | `/cpu`    | Burns CPU for ~5 seconds — used to trigger autoscaling  |

## Prerequisites

- **Go 1.25+** (for local development and tests)
- **Docker**
- **kubectl** configured against the K3s cluster
- **Helm 3+** (to install the monitoring stack)
- **Trivy** (used by the Jenkins pipeline as an image scan gate)
- **Jenkins** with agents/tools: `go`, `docker`, `kubectl`, `trivy`
- **K3s** cluster with **KEDA** installed
- **Docker Hub** account + Jenkins credential with ID `dockerhub`
- **Tailscale** (Funnel) for internet exposure

## Local Development

```bash
# Run unit tests
go test ./...

# Run the server locally (port 8080)
go run ./app

# Verify endpoints
curl http://localhost:8080/
curl http://localhost:8080/health
curl http://localhost:8080/cpu
```

### Build & run with Docker

```bash
docker build -t autoscale-go-k3s:local .
docker run --rm -p 8080:8080 autoscale-go-k3s:local
```

## Kubernetes Deployment

> The Jenkins pipeline only applies `service.yaml` and `keda.yaml` before
> updating the image. Apply the namespace and deployment manually **once**:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/deployment.yaml
```

Then deploy the rest (or let the pipeline handle it):

```bash
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/keda.yaml
```

Verify:

```bash
kubectl get pods -n go-cicd
kubectl get svc -n go-cicd
kubectl get scaledobject -n go-cicd
```

Access the app through the NodePort (example):

```
http://<node-ip>:31048/health
```

## Autoscaling with KEDA

`k8s/keda.yaml` defines a `ScaledObject`:

| Setting         | Value                     |
| --------------- | ------------------------- |
| Target          | `go-app` Deployment       |
| Min replicas    | 1                         |
| Max replicas    | 5                         |
| Trigger         | CPU Utilization           |
| Threshold       | 50%                       |

### Test the scale-up

Generate CPU load on one pod:

```bash
curl http://<node-ip>:31048/cpu
```

While load is running, watch replicas scale out:

```bash
kubectl get pods -n go-cicd -w
kubectl get scaledobject go-app-scaledobject -n go-cicd
```

## Monitoring (Prometheus & Grafana)

Run Prometheus and Grafana on the same K3s cluster using
`kube-prometheus-stack`. It bundles Prometheus, Grafana, Alertmanager,
kube-state-metrics, and node-exporter.

```
 node-exporter   kube-state-metrics   cAdvisor (pod CPU)   KEDA operator
       │                │                    │                   │
       └────────────────┴───────────┬────────┴───────────────────┘
                                    ▼
                              Prometheus  ◄──── Grafana dashboards
```

### Install with Helm

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  --namespace monitoring --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false
```

> The two `*SelectorNilUsesHelmValues=false` flags let Prometheus discover
> `ServiceMonitor`/`PodMonitor` objects across the cluster (by default it only
> selects those carrying the Helm release label). This is required to scrape
> KEDA metrics from the `keda` namespace.

Verify everything is running:

```bash
kubectl get pods -n monitoring
kubectl get svc -n monitoring
```

Wait until all pods are `Running`/`Ready` (Prometheus, Grafana, Alertmanager,
node-exporter, kube-state-metrics, operator).

### Access Grafana

Port-forward the Grafana service:

```bash
kubectl port-forward -n monitoring svc/monitoring-grafana 3000:80
```

Open <http://localhost:3000> and log in with the default credentials:

| User  | Password     |
| ----- | ------------ |
| admin | prom-operator |

If the password was changed, retrieve it from the secret:

```bash
kubectl get secret monitoring-grafana -n monitoring \
  -o jsonpath='{.data.admin-password}' | base64 --decode; echo
```

Alternative access options:

```bash
# Option A: expose Grafana as a NodePort at install time
helm upgrade --install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  --set grafana.service.type=NodePort \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false

# Option B: keep it local and expose via Tailscale (same approach as the app)
```

### Access Prometheus

```bash
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
```

Open <http://localhost:9090> and check **Status → Targets** to confirm all
targets (node-exporter, kube-state-metrics, cAdvisor, KEDA) are `UP`.

### Scrape KEDA metrics (optional)

KEDA exposes Prometheus metrics such as `keda_scaler_active` and
`keda_scaler_metrics_value` on port `8080` (`/metrics`). How you enable scraping
depends on how KEDA was installed:

**Installed with Helm:**

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm repo update

helm upgrade keda kedacore/keda -n keda --reuse-values \
  --set prometheus.operator.enabled=true \
  --set prometheus.operator.serviceMonitor.enabled=true
```

**Installed with the quickstart manifest:** apply a `ServiceMonitor` manually:

```bash
kubectl apply -f - <<'EOF'
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: keda-operator
  namespace: monitoring
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: keda-operator
  namespaceSelector:
    matchNames:
      - keda
  endpoints:
    - port: metrics
      path: /metrics
      interval: 30s
EOF
```

Verify KEDA metrics are being scraped:

```bash
kubectl get servicemonitor -n monitoring
# in Prometheus UI, run:
#   keda_scaler_active
#   keda_scaler_metrics_value
```

### Autoscaling dashboards

`kube-prometheus-stack` provisions curated dashboards in Grafana (search for
**Kubernetes / Compute Resources** and **Kubernetes / Views** in
**Dashboards → Browse**).

To track the `go-app` scale-up, add panels (or use *Explore*) with these
PromQL queries:

| Panel | PromQL |
| ----- | ------ |
| Current replicas | `sum(kube_deployment_status_replicas{namespace="go-cicd",deployment="go-app"})` |
| Desired replicas (HPA) | `sum(kube_horizontalpodautoscaler_status_desired_replicas{namespace="go-cicd"})` |
| Pod CPU usage | `sum(rate(container_cpu_usage_seconds_total{namespace="go-cicd",pod=~"go-app-.*",container!="",container!="POD"}[2m]))` |
| Pod CPU limit | `sum(kube_pod_container_resource_limits_cpu_cores{namespace="go-cicd",pod=~"go-app-.*"})` |
| KEDA scaler active | `keda_scaler_active{scaledObject="go-app-scaledobject"}` |

Live test:

```bash
# terminal 1: generate load
curl http://<node-ip>:31048/cpu

# terminal 2: watch it scale
kubectl get pods -n go-cicd -w
```

Then watch the **Current replicas** and **Pod CPU usage** panels rise, and
`keda_scaler_active` flip to `1`.

### Uninstall monitoring

```bash
helm uninstall monitoring -n monitoring
```

> Helm does not delete the chart's CRDs. Remove them manually only if nothing
> else in the cluster uses the Prometheus Operator:
>
> ```bash
> kubectl delete crd alertmanagerconfigs.monitoring.coreos.com \
>   alertmanagers.monitoring.coreos.com podmonitors.monitoring.coreos.com \
>   probes.monitoring.coreos.com prometheusagents.monitoring.coreos.com \
>   prometheuses.monitoring.coreos.com prometheusrules.monitoring.coreos.com \
>   scrapeconfigs.monitoring.coreos.com servicemonitors.monitoring.coreos.com \
>   thanosrulers.monitoring.coreos.com
> ```

## CI/CD Pipeline (Jenkins)

The declarative pipeline in `Jenkinsfile` runs these stages:

| # | Stage             | Description                                                              |
| - | ----------------- | ------------------------------------------------------------------------ |
| 1 | Checkout          | Fetch source from SCM (GitHub webhook via Tailscale Funnel)              |
| 2 | Go Test           | `go test ./...`                                                          |
| 3 | Build Image       | `docker build` tagged `${BUILD_NUMBER}` and `latest`                     |
| 4 | Trivy Image Scan  | Fails the build on HIGH/CRITICAL, fixable vulnerabilities                |
| 5 | Push Image        | Login with `dockerhub` credential, push both tags to Docker Hub          |
| 6 | Deploy to K3s     | `kubectl apply` service + KEDA, `set image`, wait for rollout (120s)     |
| 7 | Health Check      | `curl --fail --retry 10` against the app health endpoint                 |

### Environment variables

| Variable      | Value                          |
| ------------- | ------------------------------ |
| `IMAGE_REPO`  | `mafifdev/autoscale-go-k3s`    |
| `NAMESPACE`   | `go-cicd`                      |
| `DEPLOYMENT`  | `go-app`                       |
| `CONTAINER`   | `go-app`                       |

### Parameters

| Parameter | Default                                        | Description                       |
| --------- | ---------------------------------------------- | --------------------------------- |
| `APP_URL` | `http://192.168.123.240:31048/health`          | Health check URL after deploy     |

> Note: the health check stage currently uses a hardcoded URL; keep it in sync
> with your cluster node IP if it changes.

### Required Jenkins credential

| ID          | Type                | Usage                    |
| ----------- | ------------------- | ------------------------ |
| `dockerhub` | Username & Password | Docker Hub login & push  |

## Useful Commands

```bash
# Rollout status
kubectl rollout status deployment/go-app -n go-cicd

# Pod logs
kubectl logs -f deployment/go-app -n go-cicd

# Scale manually (KEDA will reconcile afterwards)
kubectl scale deployment/go-app --replicas=3 -n go-cicd

# Check KEDA trigger health
kubectl describe scaledobject go-app-scaledobject -n go-cicd
```

## Troubleshooting

| Symptom | Check |
| ------- | ----- |
| Pipeline fails at Trivy stage | Fix HIGH/CRITICAL vulnerabilities or adjust scan policy |
| Deploy stage timeout | `kubectl describe pod -n go-cicd` — likely image pull or resource issues |
| Health check fails | Verify Service NodePort and node IP; retry with `curl -v` |
| Pods not scaling | Ensure KEDA is installed and `kubectl get scaledobject -n go-cicd` shows `Ready` |
| Monitoring pods `Pending` | K3s node is low on CPU/memory — reduce replicas or free resources |
| `keda_*` metrics missing | Recheck the `ServiceMonitor` and the `*SelectorNilUsesHelmValues=false` flags |
| Cannot log in to Grafana | Reset password from the `monitoring-grafana` secret (see above) |
