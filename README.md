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
