# Infra-Sim

Infra-Sim is a deterministic discrete-event simulator for exploring throughput, latency, queueing, and bottlenecks in AWS-style architectures.

The MVP supports a YAML-defined `Load Generator -> ALB -> EC2/ECS -> RDS PostgreSQL` DAG, constant and ramp workload segments, seeded service-time distributions, fixed-time database latency degradation, streaming metrics, and plateau-oriented bottleneck reporting.

## Run

```powershell
go run ./cmd/infra-sim validate testdata/architectures/alb-ec2-rds.yaml
go run ./cmd/infra-sim run testdata/architectures/alb-ec2-rds.yaml --seed 42 --duration 30s
go run ./cmd/infra-sim run testdata/architectures/alb-ec2-rds.yaml --seed 42 --duration 30s --out result.json
go run ./cmd/infra-sim report result.json
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

The browser editor opens with a blank canvas. Build an architecture by adding AWS service nodes and drawing request-path connections, or choose **Load YAML file** to import an existing Infra-Sim `.yaml`/`.yml` architecture through the same compiler used by the CLI. Imported resource settings, workload phases, connections, and failures are restored into the editor.

The studio also supports editing resource capacity and latency, defining contiguous workload phases, scheduling latency failures, validating the graph, running the simulator, and inspecting throughput, latency, and bottleneck results.

### Development

Run the Go API and Vite development server in separate terminals:

```powershell
# Terminal 1, repository root
$env:GOCACHE="$PWD/.gocache"
$env:GOMODCACHE="$PWD/.gomodcache"
go run ./cmd/infra-sim-web

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
go run ./cmd/infra-sim-web
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
