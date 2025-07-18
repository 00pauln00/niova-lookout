# niova-lookout

`niova-lookout` is a distributed endpoint monitoring system built in Go. It monitors local applications running on a specified port range, coordinates with peer nodes via the Serf gossip protocol, and exposes live status and metrics via HTTP endpoints.

## ✨ Features

- Lightweight and distributed via Serf
- Monitors local apps and exports Prometheus-compatible metrics
- Handles endpoint discovery and dynamic updates via gossip
- HTTP interface with multiple endpoints for metrics, discovery, and health

## 🔧 Build Instructions

```bash
make
```

This builds the binary to `./niova-lookout` using the Go source in `cmd/lookout`.

## 🚀 Run Instructions

```bash
./niova-lookout \
  -dir /tmp/.niova \
  -std \
  -pmdb \
  -u 1054 \
  -hp 6666 \
  -p "7000 7100" \
  -n agent-name \
  -a 127.0.0.1 \
  -c ./gossip.txt \
  -pr ./targets.json \
  -s serf.log \
  -r raft-uuid \
  -lu lookout-uuid \
  -log debug
```

### 📘 Common Flags

| Flag          | Description                                                                 |
|---------------|-----------------------------------------------------------------------------|
| `-dir`        | Root directory for endpoint files                                           |
| `-std`        | Run in standalone mode for NISD                                             |
| `-pmdb`       | Enable PMDB support                                                         |
| `-u`          | UDP port for NISD communication                                             |
| `-hp`         | HTTP port to expose `/metrics`, `/lookouts`, `/v0`, `/v1`                   |
| `-p`          | Port range monitored (space-separated: `"7000 7100"`)                       |
| `-n`          | Unique agent name                                                           |
| `-a`          | Agent bind IP                                                               |
| `-c`          | Path to gossip config file for peer discovery                               |
| `-pr`         | Path to Prometheus targets config (usually a port in string form)           |
| `-s`          | Path to Serf log file                                                       |
| `-r`          | Raft UUID                                                                   |
| `-l`          | Lookout log file                                                            |
| `-lu`         | Lookout UUID                                                                |
| `-log`        | Log level (`panic`, `fatal`, `error`, `warn`, `info`, `debug`, `trace`)     |

## 🌐 HTTP Endpoints

| Endpoint         | Description                                                                      |
|------------------|----------------------------------------------------------------------------------|
| `/metrics`       | Prometheus-formatted metrics from all active endpoints                           |
| `/lookouts`      | JSON list of peer monitoring nodes known via Serf                                |
| `/v0/`           | Root-level metadata of monitored apps                                            |
| `/v0/<uuid>`     | Metadata for a specific app monitored by UUID                                    |
| `/v1/`           | Accepts binary-encoded queries for a specific monitored app via UUID and command |

All endpoints are served on the port defined via `-hp` (e.g. `http://localhost:6666/metrics`).

### 📤 POST /v1/

- **Content-Type:** `application/octet-stream`
- **Body:** Gob-encoded `LookoutRequest` struct

``` GO
type LookoutRequest struct {
    UUID uuid.UUID // UUID of the monitored app
    Cmd  string    // Command to execute (e.g., "GET /.*/.*/.*")
}
```

Example:
curl -X POST localhost:6666/v1/   -H "Content-Type: application/octet-stream"   --data-binary "@request.gob"


## 📁 Gossip Node File Format

The file passed to `-c` (e.g. `./gossipNodes`) should contain:

```
<space-separated IPs>
<start_port> <end_port>
```

Example:

```
127.0.0.1 192.168.1.5
7000 7100
```

## 📤 Prometheus Configuration

`niova-lookout` exposes Prometheus-compatible metrics on `/metrics`.

Your `targets.json` (used internally) should contain the port as a string:
```json
"6666"
```

Your `prometheus.yml` should look something like:

```yaml
scrape_configs:
  - job_name: 'niova-lookout'
    static_configs:
      - targets: ['localhost:6666']
```

## 🧠 About

- `Epc` refers to an endpoint container, encapsulating the metadata and metrics of a monitored app.
- The `epMap` contains all such endpoints currently tracked by a single lookout instance.
- `Lookouts` are peer monitoring agents discovered via Serf and exposed via the `/lookouts` endpoint.

