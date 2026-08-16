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
