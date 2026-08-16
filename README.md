# ArchPulse

ArchPulse is a deterministic discrete-event simulator for exploring throughput, latency, queueing, and bottlenecks in AWS-style architectures.

The MVP supports a YAML-defined `Load Generator -> ALB -> EC2/ECS -> RDS PostgreSQL` DAG, constant and ramp workload segments, seeded service-time distributions, fixed-time database latency degradation, streaming metrics, and whole-run topology-aware bottleneck reporting.

## Installation

### Prerequisites

- [Go](https://go.dev/dl/) 1.25 or newer
- [Node.js](https://nodejs.org/) 20.19 or newer (Node 22.12+ is also supported)
- Git

### Clone and install dependencies

```bash
git clone <repository-url> archpulse
cd archpulse
go mod download
cd web
npm install
npm run build
cd ..
```

The frontend must be built before using the production-style web server because
the Go application serves static files from `web/dist`.

### Start the web application

```bash
go run ./cmd/archpulse-web --addr :8080
```

Open `http://localhost:8080`. To use another port, replace `:8080`, for example:

```bash
go run ./cmd/archpulse-web --addr :8081
```

### Build standalone executables

On macOS or Linux:

```bash
mkdir -p bin
go build -o bin/archpulse ./cmd/archpulse
go build -o bin/archpulse-web ./cmd/archpulse-web
./bin/archpulse-web --web-dir web/dist --addr :8080
```

On Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force bin | Out-Null
go build -o bin/archpulse.exe ./cmd/archpulse
go build -o bin/archpulse-web.exe ./cmd/archpulse-web
.\bin\archpulse-web.exe --web-dir web/dist --addr :8080
```

For a reproducible clean frontend install using the committed lockfile, use
`npm ci` instead of `npm install`.

## Run

```powershell
go run ./cmd/archpulse validate testdata/architectures/alb-ec2-rds.yaml
go run ./cmd/archpulse run testdata/architectures/alb-ec2-rds.yaml --seed 42 --duration 30s
go run ./cmd/archpulse run testdata/architectures/alb-ec2-rds.yaml --seed 42 --duration 30s --out result.json
go run ./cmd/archpulse report result.json
```

CLI runs use the same automatic weighted sampling, graph-aware bottleneck analyzer,
and default safety limits as the web runner. The text report includes sampling,
throughput drops, the named primary bottleneck, its classification and evidence,
and other contributing constraints. Useful controls include:

```powershell
# Disable sampling (also disable limits explicitly if a very large exact run is intentional)
go run ./cmd/archpulse run architecture.yaml --exact --max-events 0 --max-queued-requests 0 --timeout 0

# Save the same enriched result used by the web application
go run ./cmd/archpulse run architecture.yaml --out result.json
go run ./cmd/archpulse report result.json
```

The full example reaches 50,000 RPS and is intentionally substantial. Use `--duration` or `--traffic` for quicker development runs.

## Verify

```powershell
$env:GOCACHE="$PWD/.gocache"
$env:GOMODCACHE="$PWD/.gomodcache"
go test ./...
go vet ./...
```

See `docs/high-level-design.md` and `docs/low-level-design.md` for the architecture and implementation contract. The LLD is authoritative where the documents differ.

## Web architecture studio

The browser editor opens with a blank canvas. Build an architecture by adding AWS service nodes and drawing request-path connections, or choose **Load YAML file** to import an existing ArchPulse `.yaml`/`.yml` architecture through the same compiler used by the CLI. Imported resource settings, workload phases, connections, and failures are restored into the editor.

The studio also supports assigning human-readable service names and editing resource capacity and latency from the persistent right-side inspector or an expanded modal editor, defining contiguous workload phases, scheduling whole-service or replica-specific latency failures, validating the graph, running the simulator, and inspecting throughput, latency, and bottleneck results. Completed simulations can be replayed on the canvas with play/pause, timeline scrubbing, selectable playback speed, and a synchronized requests-per-second graph; node colors, replica indicators, and labels follow their recorded utilization and queue pressure.

Web simulations run as background jobs. During a run, the canvas shows virtual-time progress, completed and queued request counts, animated request paths, and live node pressure: green for active capacity, yellow from 50% utilization, and red from 85% utilization or when queueing begins. Runs can be cancelled from the header.

To protect the local process from accidental overload, runs default to eight million processed events, 250,000 stored queued requests, and two minutes of wall-clock execution. Web execution additionally allows at most two concurrent simulations. Crossing a limit fails the run with an explicit message. CLI limits can be changed with `--max-events`, `--max-queued-requests`, and `--timeout`; zero disables the corresponding limit.

Large workloads automatically use deterministic weighted sampling in both the CLI and web application. The runners estimate offered arrivals and target about 200,000 representative requests; workload rates and resource capacities are scaled by the same factor, while throughput, completed-request, rejection, and queue metrics are weighted back to their represented values. Small runs remain exact (`1×`), and CLI users can request an exact run with `--exact`.

### Development

Run the Go API and Vite development server in separate terminals:

```powershell
# Terminal 1, repository root
$env:GOCACHE="$PWD/.gocache"
$env:GOMODCACHE="$PWD/.gomodcache"
go run ./cmd/archpulse-web

# Terminal 2
cd web
npm install
npm run dev
```

Open `http://localhost:5173`. Vite proxies `/api` requests to the Go server at `http://localhost:8080`.

### Production-style local run

```powershell
cd web
npm install
npm run build
cd ..
go run ./cmd/archpulse-web
```

Open `http://localhost:8080`. The production frontend is served from `web/dist`; use `--web-dir` to override that location and `--addr` to change the listen address.

### Web verification

```powershell
cd web
npm run check
npm run build
cd ..
go test ./...
```
