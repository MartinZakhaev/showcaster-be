# Showcaster BE — Load Testing

End-to-end load testing setup using **k6** for traffic generation and
**Prometheus + Grafana** for metrics collection and visualisation.

---

## Architecture

```
┌─────────────┐   HTTP traffic   ┌──────────────────┐
│   k6 VUs    │ ───────────────► │  showcaster-be   │
└─────────────┘                  │  :8080           │
       │                         │  /metrics        │
       │ remote-write             └────────┬─────────┘
       ▼                                   │ scrape
┌─────────────┐ ◄─────────────────────────┘
│ Prometheus  │  :9090
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Grafana   │  :3000  (admin / admin)
└─────────────┘
```

---

## Prerequisites

| Tool | Install |
|------|---------|
| k6   | `brew install k6` |
| Docker + Compose | [docker.com](https://www.docker.com) |

---

## Quick Start

### 1. Start the server

```bash
cd apps/showcaster-be
CGO_ENABLED=1 go run ./cmd/server
```

Verify metrics are exposed:
```bash
curl http://localhost:8080/metrics | head -20
```

### 2. Start Prometheus + Grafana

```bash
cd apps/showcaster-be/loadtest
docker compose up -d
```

Open **http://localhost:3000** → login with `admin / admin` →
navigate to **Dashboards → Showcaster → Showcaster Load Test**.

### 3. Run a load test

```bash
cd apps/showcaster-be/loadtest
```

#### Health endpoint baseline
```bash
k6 run k6/scenarios/01_health.js
```

#### Auth endpoints
```bash
k6 run k6/scenarios/02_auth.js
```

#### Job endpoints
```bash
k6 run k6/scenarios/03_jobs.js
```

#### Spike test
```bash
k6 run k6/scenarios/04_spike.js
```

#### Soak test (default 30 min — use DURATION for shorter runs)
```bash
k6 run --env DURATION=5m k6/scenarios/05_soak.js
```

### 4. Stream metrics to Grafana in real time

Add `--out experimental-prometheus-rw` to push k6 metrics into Prometheus:

```bash
k6 run \
  --out experimental-prometheus-rw \
  --env K6_PROMETHEUS_RW_SERVER_URL=http://localhost:9090/api/v1/write \
  k6/scenarios/03_jobs.js
```

### 5. Tear down

```bash
docker compose down
```

---

## Test Scenarios

| File | Type | VUs | Duration | What it tests |
|------|------|-----|----------|---------------|
| `01_health.js` | Baseline | 50 | ~2 min | Raw server throughput, no auth |
| `02_auth.js` | Load | 20 | ~3 min | Register, login, resend-OTP |
| `03_jobs.js` | Load | 10 | ~3 min | Full job CRUD (create, list, get) |
| `04_spike.js` | Spike | 0→200 | ~2 min | Sudden burst, recovery |
| `05_soak.js` | Soak | 20 | 30 min | Memory/goroutine leaks |

---

## Metrics Collected

### HTTP metrics (from `fiberprometheus`)

| Metric | Description |
|--------|-------------|
| `showcaster_be_requests_total` | Request count by method, path, status |
| `showcaster_be_request_duration_seconds` | Latency histogram by path |
| `showcaster_be_requests_in_progress_total` | Current in-flight requests |

### Go runtime metrics (from `prometheus/client_golang`)

| Metric | Description |
|--------|-------------|
| `go_goroutines` | Number of live goroutines |
| `go_memstats_heap_alloc_bytes` | Heap memory in use |
| `go_memstats_heap_sys_bytes` | Heap memory reserved from OS |
| `go_gc_duration_seconds` | GC pause duration histogram |
| `go_threads` | OS threads |

### k6 metrics (via remote-write)

| Metric | Description |
|--------|-------------|
| `k6_vus` | Active virtual users |
| `k6_http_req_duration` | End-to-end request duration histogram |
| `k6_http_req_failed` | Failed request rate |
| `k6_checks_total` | Pass/fail counts for all `check()` calls |

---

## Thresholds (pass/fail criteria)

Each scenario defines its own thresholds. The defaults are:

| Threshold | Value |
|-----------|-------|
| `http_req_duration p(95)` | < 500 ms |
| `http_req_failed` | < 1% |

k6 exits with code `99` if any threshold is breached — useful for CI gates.

---

## Grafana Dashboard Panels

1. **HTTP Request Rate** — req/s broken down by method, path, status code
2. **HTTP Duration p50/p95/p99** — latency percentiles per endpoint
3. **HTTP Error Rate** — 4xx and 5xx rates as percentages
4. **In-Flight Requests** — concurrent requests at any moment
5. **Go Goroutines** — goroutine count over time (watch for leaks)
6. **Go Heap Memory** — alloc vs sys heap in MB
7. **Go GC Pause** — garbage collection overhead
8. **k6 Virtual Users** — VU ramp profile
9. **k6 Request Duration p95** — client-side latency
10. **k6 Checks Passed Rate** — assertion success percentage
