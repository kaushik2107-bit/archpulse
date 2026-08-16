# Infra-Sim: Low-Level Design (MVP scope: Load Generator → ALB → EC2 → RDS)

Scope note: this LLD targets Phase 1 + Phase 2 from the HLD roadmap — the kernel and the four MVP resources. Later resources (Kafka, SQS, Redis, autoscaling, retries) slot into the same interfaces without changing anything below; where relevant I've noted the extension point rather than designing it now.

Language: Go, per the HLD recommendation. Types below are real Go, not pseudocode, so this maps directly to files.

---

## 1. Repository Structure

```
infra-sim/
├── cmd/
│   └── infra-sim/
│       └── main.go                 # CLI entrypoint, cobra command tree
├── internal/
│   ├── kernel/                     # Phase 1 — knows nothing about AWS
│   │   ├── clock.go
│   │   ├── event.go
│   │   ├── eventqueue.go
│   │   ├── engine.go
│   │   ├── resource.go             # SimResource interface
│   │   ├── request.go
│   │   ├── rng.go                  # seeded sub-stream PRNG
│   │   └── kernel_test.go          # M/M/1, M/M/c closed-form validation
│   ├── resources/                  # Phase 2 — generic simulation classes, AWS-agnostic
│   │   ├── registry.go
│   │   ├── serverpool.go           # shared primitive
│   │   ├── connectionpool.go       # shared primitive
│   │   ├── loadgenerator.go
│   │   ├── loadbalancer.go
│   │   ├── compute.go
│   │   └── database.go
│   ├── profiles/                   # AWS service catalog — the seam described in §9.6
│   │   ├── catalog.go              # ServiceProfile registry keyed by "aws.*" names
│   │   ├── ec2.go
│   │   ├── ecs.go
│   │   ├── rds_postgres.go
│   │   ├── alb.go
│   │   └── route53.go
│   ├── ir/                         # Infrastructure IR + YAML compiler
│   │   ├── types.go
│   │   ├── yaml_compiler.go
│   │   └── validate.go
│   ├── workload/
│   │   ├── generator.go            # interface
│   │   ├── constant.go
│   │   └── ramp.go
│   ├── metrics/
│   │   ├── sink.go
│   │   ├── histogram.go            # HDR histogram wrapper
│   │   └── timeseries.go
│   ├── analysis/
│   │   ├── bottleneck.go
│   │   └── plateau.go
│   ├── failure/
│   │   └── injector.go             # MVP: single fixed-time latency injection
│   └── report/
│       └── render.go               # text + json output
├── pkg/
│   └── model/                      # exported result types (for future API/UI reuse)
│       └── result.go
├── testdata/
│   └── architectures/
│       └── alb-ec2-rds.yaml
├── go.mod
└── README.md
```

Rule enforced by this layout: `internal/kernel` has zero imports from `internal/resources`, `internal/ir`, or `internal/workload`. This is the physical embodiment of the HLD's "dependencies point downward" rule — if you ever `import "infra-sim/internal/resources"` inside `kernel/`, that's a design violation, and Go's own compiler + a simple lint rule (or just `go list -deps` in CI) can enforce it mechanically.

---

## 2. Kernel: Core Types

### 2.1 Clock and time representation

```go
// internal/kernel/clock.go
package kernel

// SimTime is virtual nanoseconds since simulation start. Never wall-clock.
type SimTime int64

const (
	Nanosecond  SimTime = 1
	Microsecond         = 1000 * Nanosecond
	Millisecond         = 1000 * Microsecond
	Second               = 1000 * Millisecond
)
```

Using integer nanoseconds (not float) deliberately — avoids float non-associativity issues affecting event ordering (HLD §11/§20.6), and `int64` gives ~292 years of range at nanosecond precision, more than enough headroom.

### 2.2 Event

```go
// internal/kernel/event.go
package kernel

type EventType int

const (
	RequestArrived EventType = iota
	RoutingDecision
	QueueEnqueued
	ServiceStarted
	ServiceCompleted
	DownstreamCallStarted
	DownstreamCallCompleted
	ResponseSent
	RequestRejected
	RequestTimedOut
	TimeoutCheck
	ResourceDegraded
	ResourceRecovered
	PolicyTick
)

type EventID uint64

type Event struct {
	Time         SimTime
	Seq          uint64          // tiebreaker, assigned at creation (see EventQueue)
	Type         EventType
	Target       ResourceID
	Payload      any             // concrete payload struct per EventType, see below
	CausalParent EventID         // 0 = root event (external arrival)
	id           EventID         // assigned by queue on push
}

// Payload types — one per EventType that needs data beyond Target.
type RequestArrivedPayload struct {
	Request *RequestState
}

type ServiceCompletedPayload struct {
	Request *RequestState
}

type DownstreamCallPayload struct {
	Request  *RequestState
	Upstream ResourceID // who to notify on completion
}

type ResourceDegradedPayload struct {
	LatencyMultiplier float64
}
```

`any` for `Payload` is a deliberate simplicity/type-safety tradeoff for v1 — a type switch in each resource's `HandleEvent` does the work. If this gets unwieldy once more event types exist (Phase 3+), revisit with generics (`Event[T]`) or a discriminated union via interface + type assertion helpers. Not worth the complexity now.

### 2.3 Request state

```go
// internal/kernel/request.go
package kernel

type RequestID uint64

type RequestState struct {
	ID            RequestID
	ArrivalTime   SimTime
	CurrentHop    ResourceID
	Upstream      ResourceID    // who to reply to when this request completes at its
	                            // current hop — set every time a request is handed to
	                            // a new resource (see engine.go dispatch note below).
	                            // Fixes the "waiting request forgets its caller" gap
	                            // flagged in the original §5.4 draft.
	PathHistory   []HopRecord   // for debugging / cascade trace, bounded — see §6
	OperationType string        // "read" | "write", used by DatabaseResource
	SizeBytes     int64
	Deadline      SimTime       // 0 = no deadline (MVP: no timeouts, so unused for now)
	RetriesSoFar  int           // unused in MVP, present for Phase 4 forward-compat
}

type HopRecord struct {
	Resource ResourceID
	EnterAt  SimTime
	ExitAt   SimTime // 0 if still in-flight
}
```

### 2.4 Event queue

```go
// internal/kernel/eventqueue.go
package kernel

import "container/heap"

type EventQueue struct {
	heap    eventHeap
	nextSeq uint64
	nextID  EventID
}

func NewEventQueue() *EventQueue {
	q := &EventQueue{}
	heap.Init(&q.heap)
	return q
}

// Push assigns Seq and id deterministically at push time, in call order.
// Callers must push events in a deterministic order within a tick — the
// engine's dispatch loop guarantees this (see §3).
func (q *EventQueue) Push(e Event) EventID {
	e.Seq = q.nextSeq
	q.nextSeq++
	q.nextID++
	e.id = q.nextID
	heap.Push(&q.heap, e)
	return e.id
}

func (q *EventQueue) Pop() (Event, bool) {
	if q.heap.Len() == 0 {
		return Event{}, false
	}
	return heap.Pop(&q.heap).(Event), true
}

func (q *EventQueue) Len() int { return q.heap.Len() }

// --- container/heap plumbing ---
type eventHeap []Event

func (h eventHeap) Len() int { return len(h) }
func (h eventHeap) Less(i, j int) bool {
	if h[i].Time != h[j].Time {
		return h[i].Time < h[j].Time
	}
	return h[i].Seq < h[j].Seq // deterministic tiebreak
}
func (h eventHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *eventHeap) Push(x any)         { *h = append(*h, x.(Event)) }
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
```

Note on `Seq` assignment: assigning it at **push time**, in the order the engine calls `Push`, is sufficient for determinism as long as the engine itself always processes a given tick's resulting events in a fixed order (see §3.2) — we don't need "assigned at creation" as originally sketched in the HLD; push-order is simpler to implement correctly and equivalent in practice as long as push order is itself deterministic, which it is here.

### 2.5 Resource interface

```go
// internal/kernel/resource.go
package kernel

type ResourceID uint32 // dense integer, cheap array indexing — see §2.6

type SimResource interface {
	ID() ResourceID
	HandleEvent(ev Event, ctx *SimContext) []Event
	SnapshotMetrics() ResourceMetricsSnapshot
	ApplyFailure(f ResourceDegradedPayload)
	ClearFailure()
}

type ResourceMetricsSnapshot struct {
	ResourceID       ResourceID
	InFlight         int
	QueueDepth       int
	Capacity         int
	UtilizationPct   float64
	// resource-specific extras go in a map to avoid a bloated shared struct
	Extra            map[string]float64
}
```

### 2.6 World state / resource registry

```go
// internal/kernel/engine.go (continued in §3)
package kernel

type World struct {
	resources []SimResource // dense array, index == ResourceID for O(1) lookup
}

// Reserve allocates the next ResourceID without a resource attached yet.
// Set attaches the resource once it's constructed. This two-phase split is
// the ONLY registration API — it exists because §9.5's BuildWorld needs a
// node's downstream ResourceID before that downstream node has been
// constructed (compute needs to know the database's ID at construction
// time; the database isn't built yet on the first pass). A single-step
// Register() can't support that, so there is no single-step alternative —
// every caller, including the trivial single-resource case, goes through
// Reserve() then Set().
func (w *World) Reserve() ResourceID {
	id := ResourceID(len(w.resources))
	w.resources = append(w.resources, nil) // placeholder, filled by Set
	return id
}

func (w *World) Set(id ResourceID, r SimResource) {
	w.resources[id] = r
}

func (w *World) Get(id ResourceID) SimResource { return w.resources[id] }

// Len supports iteration for metrics sink initialization (§6) and the
// PolicyTick handling described in §3.
func (w *World) Len() int { return len(w.resources) }
```

Dense array indexed by `ResourceID` (not a map) — this is the HLD §2 recommendation made concrete; avoids hashmap overhead in the hottest lookup path (every single event dispatch does this lookup).

### 2.7 Deterministic PRNG sub-streams

```go
// internal/kernel/rng.go
package kernel

import (
	"hash/fnv"
	"math/rand"
)

type RNGStreams struct {
	Workload   *rand.Rand
	ServiceTime *rand.Rand
	Failure    *rand.Rand
}

func NewRNGStreams(masterSeed int64) *RNGStreams {
	return &RNGStreams{
		Workload:    rand.New(rand.NewSource(deriveSeed(masterSeed, "workload"))),
		ServiceTime: rand.New(rand.NewSource(deriveSeed(masterSeed, "servicetime"))),
		Failure:     rand.New(rand.NewSource(deriveSeed(masterSeed, "failure"))),
	}
}

func deriveSeed(master int64, name string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(name))
	mix := h.Sum64() ^ uint64(master)
	return int64(mix)
}
```

Each resource/generator that needs randomness gets a reference to the relevant stream via `SimContext` (below), never a fresh unseeded RNG, never `time.Now()`-seeded anything. This directly implements HLD §11.

### 2.8 SimContext — what a resource can do when handling an event

```go
type SimContext struct {
	Now     SimTime
	RNG     *RNGStreams
	World   *World
	Metrics *metrics.Sink   // append-only observe calls, see §6
}
```

Passed by pointer into every `HandleEvent` call. Resources schedule follow-up events by _returning_ them (`[]Event`), not by reaching into the engine's queue directly — keeps resources unit-testable in isolation (call `HandleEvent`, assert on the returned events, no engine needed).

---

## 3. Kernel: Engine Loop

```go
// internal/kernel/engine.go
package kernel

type Engine struct {
	Queue   *EventQueue
	World   *World
	RNG     *RNGStreams
	Metrics *metrics.Sink
	Horizon SimTime
	now     SimTime
}

// RunTrace is the kernel's own output — just the numbers needed to prove
// determinism (§3.2) and to feed §10.2's regression test. It does NOT embed
// *metrics.Sink; the Sink lives on Engine and is read directly by whatever
// calls Run() (see §14's Bootstrap, and §12's Analyze, which now takes the
// Sink as its second argument to match how it's actually called).
type RunTrace struct {
	TotalEventsProcessed uint64
	FinalTime            SimTime
}

func (e *Engine) Run() RunTrace {
	var processed uint64
	for e.Queue.Len() > 0 {
		ev, _ := e.Queue.Pop()
		if ev.Time > e.Horizon {
			break // discard events past horizon, don't process them
		}
		e.now = ev.Time
		processed++

		// PolicyTick is engine-native, not resource-dispatched. Earlier
		// drafts routed it through world.Get(0).HandleEvent(...), which
		// silently coupled metrics sampling to whatever resource happened
		// to have ResourceID 0 — a real bug, not just an inelegance, since
		// that resource's HandleEvent would receive a PolicyTick it was
		// never designed to interpret. The engine intercepts it directly:
		if ev.Type == PolicyTick {
			e.Metrics.Observe(ev, e.World) // does the utilization/queue sampling walk, §6
			next := scheduleNextTick(e.now, e.Metrics.TickInterval())
			e.Queue.Push(next)
			continue
		}

		ctx := &SimContext{Now: e.now, RNG: e.RNG, World: e.World, Metrics: e.Metrics}
		res := e.World.Get(ev.Target)
		followUps := res.HandleEvent(ev, ctx)

		e.Metrics.Observe(ev, e.World) // §6

		// push follow-ups in deterministic order: as returned by HandleEvent.
		// HandleEvent implementations must themselves build this slice in a
		// fixed order (never range over a map when constructing it).
		//
		// Upstream/CurrentHop bookkeeping happens HERE, once, generically —
		// not left to each resource to remember. Any follow-up event that
		// hands a request to a NEW target resource gets that request's
		// Upstream set to the resource that just processed it (ev.Target),
		// and CurrentHop set to the new target. This is what lets a request
		// sitting in a ConnectionPool.Waiters queue (§4.2) still know who to
		// reply to later, closing the gap flagged against RequestState.
		for _, fu := range followUps {
			fu.CausalParent = ev.id
			if req := requestFromPayload(fu.Payload); req != nil && fu.Target != ev.Target {
				req.Upstream = ev.Target
				req.CurrentHop = fu.Target
			}
			e.Queue.Push(fu)
		}
	}
	return RunTrace{TotalEventsProcessed: processed, FinalTime: e.now}
}

// requestFromPayload is a small helper that type-switches over the known
// payload shapes to extract *RequestState when present. Centralizing this
// here (rather than duplicating Upstream/CurrentHop-setting logic inside
// every resource's HandleEvent) is what makes the bookkeeping impossible to
// forget — it happens once, at the only place events actually move between
// resources.
func requestFromPayload(p any) *RequestState {
	switch v := p.(type) {
	case RequestArrivedPayload:
		return v.Request
	case ServiceCompletedPayload:
		return v.Request
	case DownstreamCallPayload:
		return v.Request
	}
	return nil
}
```

### 3.1 Why events, not returned-and-immediately-processed calls

Every `HandleEvent` returns new events rather than recursively calling other resources' handlers directly. This is the entire mechanism that makes the engine single-threaded-simple _and_ correctly orders concurrent request processing — two requests "racing" through the system interleave correctly because they're both just entries in the same global heap, ordered by time, not by call-stack order.

### 3.2 Determinism guarantee, precisely stated

Given: same IR, same workload config, same seed, same engine binary version →

1. `EventQueue.Push` is always called in the same order (because `HandleEvent` builds its return slice deterministically, and the engine iterates that slice in order).
2. Therefore `Seq` assignment is identical across runs.
3. Therefore heap pop order is identical (time+seq is a total order).
4. Therefore RNG draws happen in identical sequence against identical seeded streams.
   → bit-identical `RunTrace` and metrics output. This is the property the kernel's regression tests (§10) directly assert on.

### 3.3 Periodic tick scheduling (metrics sampling, future autoscaling)

Still no special "timer" subsystem — a tick is a self-rescheduling event — but per §3's fix, `PolicyTick` is handled directly by the engine loop rather than dispatched to a resource, since it has no single meaningful `Target` (it's a whole-world sampling pass, not a message to one resource):

```go
func scheduleNextTick(now SimTime, interval SimTime) Event {
	return Event{Time: now + interval, Type: PolicyTick} // Target intentionally unset/unused
}
```

The bootstrap step (§14) seeds one `PolicyTick` event at `t=0` before `Run()` starts. The engine's own dispatch loop (§3) recognizes `PolicyTick` before it ever reaches `world.Get(ev.Target)`, does the sampling work via `Metrics.Observe`, and reschedules itself. When Phase 4 adds autoscaling, policy evaluation per-resource can be layered on top of this same tick without changing this mechanism — the tick stays engine-native; only what happens in response to it grows.

---

## 4. Shared Resource Primitives

### 4.1 ServerPool (used by LoadBalancer, ComputeResource)

```go
// internal/resources/serverpool.go
package resources

type ServiceTimeSampler interface {
	Sample(rng *rand.Rand) kernel.SimTime
}

// LognormalSampler is the MVP default — see HLD §20.1 on why not constant.
type LognormalSampler struct {
	MeanMs   float64
	StdDevMs float64
}

func (s LognormalSampler) Sample(rng *rand.Rand) kernel.SimTime {
	mu, sigma := lognormalParams(s.MeanMs, s.StdDevMs)
	ms := math.Exp(mu + sigma*rng.NormFloat64())
	return kernel.SimTime(ms * float64(kernel.Millisecond))
}

type ServerPool struct {
	Capacity    int
	InFlight    int
	Queue       []*kernel.RequestState // FIFO
	QueueLimit  int                    // 0 = unbounded (MVP default for compute)
	ServiceTime ServiceTimeSampler
}

type AdmitResult int

const (
	Admitted AdmitResult = iota
	Queued
	Rejected
)

func (p *ServerPool) TryAdmit(req *kernel.RequestState) AdmitResult {
	if p.InFlight < p.Capacity {
		p.InFlight++
		return Admitted
	}
	if p.QueueLimit > 0 && len(p.Queue) >= p.QueueLimit {
		return Rejected
	}
	p.Queue = append(p.Queue, req)
	return Queued
}

// OnServiceComplete frees a slot and admits the next queued request, if any.
// Returns the request that should now start service (nil if none).
func (p *ServerPool) OnServiceComplete() *kernel.RequestState {
	p.InFlight--
	if len(p.Queue) == 0 {
		return nil
	}
	next := p.Queue[0]
	p.Queue = p.Queue[1:]
	p.InFlight++
	return next
}
```

`p.Queue = p.Queue[1:]` is O(1) amortized (slice re-slicing) but leaks backing array capacity over a long run — fine for MVP run lengths (minutes of simulated time, not the queue growing unboundedly in the happy path), but flag as a known cleanup item if `QueueLimit=0` runs get long: swap to a ring buffer if profiling shows GC pressure from this.

### 4.2 ConnectionPool (used by DatabaseResource)

```go
// internal/resources/connectionpool.go
package resources

type ConnectionPool struct {
	MaxConnections int
	InUse          int
	Waiters        []*kernel.RequestState
}

func (c *ConnectionPool) Acquire(req *kernel.RequestState) AdmitResult {
	if c.InUse < c.MaxConnections {
		c.InUse++
		return Admitted
	}
	c.Waiters = append(c.Waiters, req) // MVP: unbounded waiter queue, no rejection
	return Queued
}

func (c *ConnectionPool) Release() *kernel.RequestState {
	c.InUse--
	if len(c.Waiters) == 0 {
		return nil
	}
	next := c.Waiters[0]
	c.Waiters = c.Waiters[1:]
	c.InUse++
	return next
}

func (c *ConnectionPool) UtilizationPct() float64 {
	return 100 * float64(c.InUse) / float64(c.MaxConnections)
}
```

Structurally near-identical to `ServerPool` — deliberately. In Phase 3+, consider unifying these into one generic `CapacityPool[T]` if the duplication starts to bite; kept separate for MVP because `ConnectionPool` will diverge first (visibility timeout logic for SQS, lock semantics for DB — different enough per-resource behavior that premature unification could cost more than it saves right now).

---

## 5. MVP Resources

### 5.1 LoadGenerator

Not really a `SimResource` in the request-routing sense — it's the thing that seeds `RequestArrived` events. Implemented as a resource anyway so it participates in the same event loop uniformly.

```go
// internal/resources/loadgenerator.go
package resources

type LoadGenerator struct {
	id         kernel.ResourceID
	Downstream kernel.ResourceID // e.g. the ALB
	Workload   workload.Generator
	nextReqID  kernel.RequestID
}

func (g *LoadGenerator) HandleEvent(ev kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	// Explicitly gate on event type rather than assuming every event that
	// reaches this resource is its own self-tick. An earlier draft omitted
	// this check; harmless while LoadGenerator is the only thing that ever
	// targets itself, but a latent bug the moment anything else does
	// (e.g. a future "pause traffic" control event).
	if ev.Type != kernel.RequestArrived {
		return nil
	}
	nextTime, ok := g.Workload.NextArrival(ev.Time, ctx.RNG.Workload)
	if !ok {
		return nil // workload exhausted
	}
	g.nextReqID++
	req := &kernel.RequestState{
		ID:          g.nextReqID,
		ArrivalTime: nextTime,
	}
	arrival := kernel.Event{
		Time:    nextTime,
		Type:    kernel.RequestArrived,
		Target:  g.Downstream,
		Payload: kernel.RequestArrivedPayload{Request: req},
	}
	// self-reschedule: ask again for the arrival after this one
	selfTick := kernel.Event{Time: nextTime, Type: kernel.RequestArrived, Target: g.id}
	return []kernel.Event{arrival, selfTick}
}
```

Pull-based generation (HLD §5 note) — one arrival is scheduled at a time, not the whole run pre-generated upfront. Keeps memory flat regardless of run duration/rate.

### 5.2 LoadBalancer (ALB)

```go
// internal/resources/loadbalancer.go
package resources

type LoadBalancer struct {
	id        kernel.ResourceID
	Backends  []kernel.ResourceID
	rrIndex   int // round-robin pointer; deterministic, not random choice
	pool      *ServerPool // models LB's own connection-handling overhead, generous capacity
}

func (lb *LoadBalancer) HandleEvent(ev kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch ev.Type {
	case kernel.RequestArrived:
		req := ev.Payload.(kernel.RequestArrivedPayload).Request
		backend := lb.Backends[lb.rrIndex%len(lb.Backends)]
		lb.rrIndex++
		return []kernel.Event{{
			Time:    ev.Time, // ALB routing overhead modeled as ~0 for MVP; add fixed µs cost later if calibration shows it matters
			Type:    kernel.RequestArrived,
			Target:  backend,
			Payload: kernel.RequestArrivedPayload{Request: req},
		}}
	}
	return nil
}
```

Round-robin, not least-connections, for MVP — simplest correct routing policy; least-connections requires the LB to query backend queue depth, which is a legitimate Phase-3 enhancement but adds coupling not needed to prove the core model.

### 5.3 ComputeResource

```go
// internal/resources/compute.go
package resources

type ComputeResource struct {
	id         kernel.ResourceID
	Pool       *ServerPool
	Downstream kernel.ResourceID // e.g. the database
}

func (c *ComputeResource) HandleEvent(ev kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch ev.Type {
	case kernel.RequestArrived:
		req := ev.Payload.(kernel.RequestArrivedPayload).Request
		result := c.Pool.TryAdmit(req)
		switch result {
		case Rejected:
			return []kernel.Event{{Time: ev.Time, Type: kernel.RequestRejected, Target: c.id,
				Payload: kernel.RequestArrivedPayload{Request: req}}}
		case Queued:
			return nil // sits in pool.Queue, nothing scheduled until a slot frees
		case Admitted:
			return c.startDownstreamCall(ev.Time, req, ctx)
		}

	case kernel.DownstreamCallCompleted:
		req := ev.Payload.(kernel.DownstreamCallPayload).Request
		// compute finishes its own work after the DB call returns
		svcTime := c.Pool.ServiceTime.Sample(ctx.RNG.ServiceTime)
		return []kernel.Event{{Time: ev.Time + svcTime, Type: kernel.ServiceCompleted,
			Target: c.id, Payload: kernel.ServiceCompletedPayload{Request: req}}}

	case kernel.ServiceCompleted:
		req := ev.Payload.(kernel.ServiceCompletedPayload).Request
		out := []kernel.Event{{Time: ev.Time, Type: kernel.ResponseSent, Target: c.id,
			Payload: kernel.ServiceCompletedPayload{Request: req}}}
		if next := c.Pool.OnServiceComplete(); next != nil {
			out = append(out, c.startDownstreamCall(ev.Time, next, ctx)...)
		}
		return out
	}
	return nil
}

func (c *ComputeResource) startDownstreamCall(now kernel.SimTime, req *kernel.RequestState, ctx *kernel.SimContext) []kernel.Event {
	return []kernel.Event{{Time: now, Type: kernel.DownstreamCallStarted, Target: c.Downstream,
		Payload: kernel.DownstreamCallPayload{Request: req, Upstream: c.id}}}
}
```

Note the split: `RequestArrived` → admit → call DB; `DownstreamCallCompleted` (fired by the DB resource, see 5.4) → sample **compute's own** service time (application logic time, separate from DB query time) → `ServiceCompleted` → free the pool slot and pull the next queued request. This two-stage service time (compute-side processing + DB-side processing, both real and separately configurable) matches the HLD's request lifecycle chain exactly and is what lets the bottleneck analyzer later distinguish "compute is slow" from "compute is waiting on DB."

**Capacity formula (previously left implicit):** `newComputeFromParams` sets `Pool.Capacity = instances * workers_per_instance` — e.g. the walkthrough's `instances: 4, workers_per_instance: 100` yields `Capacity = 400`. This treats the compute layer as one aggregate pool rather than 4 independently-queued instances. That's a deliberate MVP simplification worth stating explicitly: it means the model can't currently show "one instance is hot while the other three are idle" (a real and common production pattern, e.g. from imperfect load-balancer routing) — every request draws from one shared pool of 400 slots regardless of which "instance" a strict physical model would say it landed on. Modeling per-instance pools (an array of 4 `ServerPool`s behind the `LoadBalancer`'s existing round-robin routing) is a natural Phase-3 enhancement if that distinction turns out to matter, and requires no kernel change — only `ComputeResource` internals.

### 5.4 DatabaseResource

```go
// internal/resources/database.go
package resources

type DatabaseResource struct {
	id           kernel.ResourceID
	ConnPool     *ConnectionPool
	QueryTime    ServiceTimeSampler
	degraded     bool
	latencyMult  float64
}

func (d *DatabaseResource) HandleEvent(ev kernel.Event, ctx *kernel.SimContext) []kernel.Event {
	switch ev.Type {
	case kernel.DownstreamCallStarted:
		p := ev.Payload.(kernel.DownstreamCallPayload)
		result := d.ConnPool.Acquire(p.Request)
		if result == Admitted {
			return d.startQuery(ev.Time, p.Request, p.Upstream, ctx)
		}
		return nil // sits in ConnPool.Waiters

	case kernel.ServiceCompleted: // internal: query finished
		payload := ev.Payload.(dbQueryDonePayload)
		out := []kernel.Event{{Time: ev.Time, Type: kernel.DownstreamCallCompleted, Target: payload.Upstream,
			Payload: kernel.DownstreamCallPayload{Request: payload.Request}}}
		if next := d.ConnPool.Release(); next != nil {
			// next.Upstream was set generically by the engine's dispatch
			// loop (§3) at the moment this request was first handed to the
			// database, so it's reliably populated even after sitting in
			// ConnPool.Waiters for an arbitrary amount of virtual time.
			out = append(out, d.startQuery(ev.Time, next, next.Upstream, ctx)...)
		}
		return out
	}
	return nil
}

func (d *DatabaseResource) startQuery(now kernel.SimTime, req *kernel.RequestState, upstream kernel.ResourceID, ctx *kernel.SimContext) []kernel.Event {
	qt := d.QueryTime.Sample(ctx.RNG.ServiceTime)
	if d.degraded {
		qt = kernel.SimTime(float64(qt) * d.latencyMult)
	}
	return []kernel.Event{{Time: now + qt, Type: kernel.ServiceCompleted, Target: d.id,
		Payload: dbQueryDonePayload{Request: req, Upstream: upstream}}}
}

func (d *DatabaseResource) ApplyFailure(f kernel.ResourceDegradedPayload) {
	d.degraded = true
	d.latencyMult = f.LatencyMultiplier
}
func (d *DatabaseResource) ClearFailure() { d.degraded = false }

type dbQueryDonePayload struct {
	Request  *kernel.RequestState
	Upstream kernel.ResourceID
}
```

This resolves what was previously flagged as a known gap (which upstream does a waiting request belong to) — `RequestState.Upstream` (§2.3) plus the engine's generic bookkeeping (§3) together mean no resource has to manually track or pass this through; it's reliably populated on the request by the time any handler needs it.

`ApplyFailure`/`ClearFailure` is the entire MVP failure-injection surface (HLD §18: single fixed-time latency injection). The injector (§8 below) just calls these at scheduled times.

---

## 6. Metrics Collection

```go
// internal/metrics/sink.go
package metrics

type Sink struct {
	perResource map[kernel.ResourceID]*ResourceMetrics
	tickIntervalNs kernel.SimTime
}

type ResourceMetrics struct {
	Throughput   *TimeSeries       // completed-request counts per 1s bucket
	Latency      *HDRHistogram     // end-to-end request latency (recorded at ResponseSent only)
	Utilization  *TimeSeries       // sampled at PolicyTick, not per-event
	QueueDepth   *TimeSeries       // sampled at PolicyTick
	Errors       int64
	Rejected     int64
}

func (s *Sink) Observe(ev kernel.Event, world *kernel.World) {
	switch ev.Type {
	case kernel.ResponseSent:
		p := ev.Payload.(kernel.ServiceCompletedPayload)
		latencyNs := ev.Time - p.Request.ArrivalTime
		s.perResource[ev.Target].Latency.Record(latencyNs)
		s.perResource[ev.Target].Throughput.Increment(ev.Time)
	case kernel.RequestRejected:
		s.perResource[ev.Target].Rejected++
	case kernel.PolicyTick:
		s.sampleUtilization(ev.Time, world)
	}
}

func (s *Sink) sampleUtilization(now kernel.SimTime, world *kernel.World) {
	for id, rm := range s.perResource {
		snap := world.Get(id).SnapshotMetrics()
		rm.Utilization.Record(now, snap.UtilizationPct)
		rm.QueueDepth.Record(now, float64(snap.QueueDepth))
	}
}

// NewSink pre-populates one ResourceMetrics per existing resource in the
// world (via World.Len(), §2.6) so Observe/sampleUtilization never index into
// a missing map entry — this constructor was referenced by name from §12's
// Bootstrap but not previously defined.
func NewSink(world *kernel.World, tickInterval kernel.SimTime) *Sink {
	s := &Sink{perResource: map[kernel.ResourceID]*ResourceMetrics{}, tickIntervalNs: tickInterval}
	for i := 0; i < world.Len(); i++ {
		id := kernel.ResourceID(i)
		s.perResource[id] = &ResourceMetrics{
			Throughput:  NewTimeSeries(),
			Latency:     NewHDRHistogram(),
			Utilization: NewTimeSeries(),
			QueueDepth:  NewTimeSeries(),
		}
	}
	return s
}

func (s *Sink) TickInterval() kernel.SimTime { return s.tickIntervalNs }

// GlobalThroughput sums per-resource throughput series into one aggregate
// series — specifically the load generator's downstream target (the ALB),
// since that's the single point every accepted request passes through
// exactly once, making it the correct proxy for "served throughput" that
// §13's plateau detector needs. (Summing across ALL resources would
// double-count every hop of every request.)
func (s *Sink) GlobalThroughput() *TimeSeries {
	return s.perResource[s.entryPointResourceID].Throughput
}

func (s *Sink) PerResource() map[kernel.ResourceID]*ResourceMetrics { return s.perResource }
```

`entryPointResourceID` (the field backing `GlobalThroughput`) needs to be threaded into `NewSink` as an extra argument once the entry-point resource is known — a one-line addition once `Bootstrap` (§12) is implemented, since `Bootstrap` already resolves `loadGenID`'s downstream target while wiring the queue. Flagging this rather than hand-waving it: which resource counts as "the" throughput to plateau-detect against is a real modeling decision (here: the ALB, the single funnel every request passes through once), not an arbitrary pick.

Deliberately **not** logging every event to a persistent trace — `Observe` does O(1) work per event (histogram record, counter increment) and only does the heavier per-resource snapshot walk on the coarse `PolicyTick` cadence (e.g. every 1s virtual time), exactly matching HLD §6's streaming-aggregation design. This is what keeps a 300s-simulated / 50k-RPS run (tens of millions of events) tractable in memory.

### 6.1 HDR Histogram wrapper

```go
// internal/metrics/histogram.go
package metrics

import hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

type HDRHistogram struct {
	h *hdrhistogram.Histogram
}

func NewHDRHistogram() *HDRHistogram {
	// 1 microsecond to 60 seconds range, 3 significant digits — plenty for
	// request latencies in this domain, tune if a resource genuinely needs
	// sub-microsecond or multi-minute latency resolution.
	return &HDRHistogram{h: hdrhistogram.New(1, 60_000_000, 3)}
}

func (hh *HDRHistogram) Record(latencyNs kernel.SimTime) {
	_ = hh.h.RecordValue(int64(latencyNs) / 1000) // store in microseconds
}

func (hh *HDRHistogram) P50() float64 { return float64(hh.h.ValueAtQuantile(50)) }
func (hh *HDRHistogram) P95() float64 { return float64(hh.h.ValueAtQuantile(95)) }
func (hh *HDRHistogram) P99() float64 { return float64(hh.h.ValueAtQuantile(99)) }
```

Use an existing, well-tested HDR histogram library rather than hand-rolling one — this is exactly the kind of component where a subtle off-by-one in bucket boundaries silently corrupts your p99 numbers, and it's not where your engineering time should go.

### 6.2 Bounded causal trace (for cascade debugging, not full logging)

```go
type CausalTrace struct {
	ring   []kernel.Event
	cap    int
	cursor int
}

func (t *CausalTrace) Append(ev kernel.Event) {
	if len(t.ring) < t.cap {
		t.ring = append(t.ring, ev)
	} else {
		t.ring[t.cursor] = ev
		t.cursor = (t.cursor + 1) % t.cap
	}
}
```

Fixed-size ring buffer (e.g. last 10,000 events), enabled only via a `--trace` debug flag for MVP — not wired into the default hot path, since even a ring-buffer append per event is overhead you don't want paid unconditionally at 50k+ RPS scale.

---

## 7. Workload Generator

```go
// internal/workload/generator.go
package workload

type Generator interface {
	// Returns the next arrival time strictly after `after`, and whether
	// the workload has more arrivals to produce.
	NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool)
}
```

### 7.1 Constant (Poisson)

```go
// internal/workload/constant.go
type Constant struct {
	RatePerSec float64
	EndTime    kernel.SimTime
}

func (c Constant) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	if after >= c.EndTime {
		return 0, false
	}
	// exponential inter-arrival: -ln(U)/lambda, lambda in events/ns
	lambdaPerNs := c.RatePerSec / 1e9
	u := rng.Float64()
	interArrivalNs := -math.Log(1-u) / lambdaPerNs
	next := after + kernel.SimTime(interArrivalNs)
	if next >= c.EndTime {
		return 0, false
	}
	return next, true
}
```

### 7.2 Ramp (time-varying Poisson via thinning)

```go
// internal/workload/ramp.go
type Ramp struct {
	StartRate, EndRate float64 // req/s
	StartTime, EndTime kernel.SimTime
}

func (r Ramp) rateAt(t kernel.SimTime) float64 {
	frac := float64(t-r.StartTime) / float64(r.EndTime-r.StartTime)
	return r.StartRate + frac*(r.EndRate-r.StartRate)
}

// Thinning algorithm: sample candidate arrivals at the MAX rate over the
// window, then probabilistically accept/reject each based on the true
// rate at that instant. Standard technique for time-varying Poisson processes.
func (r Ramp) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	maxRate := math.Max(r.StartRate, r.EndRate)
	t := after
	for {
		lambdaPerNs := maxRate / 1e9
		u := rng.Float64()
		t += kernel.SimTime(-math.Log(1-u) / lambdaPerNs)
		if t >= r.EndTime {
			return 0, false
		}
		acceptProb := r.rateAt(t) / maxRate
		if rng.Float64() < acceptProb {
			return t, true
		}
		// rejected candidate, loop continues from t
	}
}
```

This is the one piece of the MVP that's genuinely "an algorithm" rather than bookkeeping — worth the comment, and worth a dedicated unit test that samples a large number of arrivals and checks the empirical rate matches `rateAt(t)` within tolerance across several time windows (a statistical test, not an exact-equality test — see §10).

---

## 8. Failure Injector (MVP: single fixed-time latency injection)

```go
// internal/failure/injector.go
package failure

type ScheduledFailure struct {
	At                kernel.SimTime
	Target            kernel.ResourceID
	LatencyMultiplier float64
}

// Seeds two events into the queue up front: one to apply, one to clear
// (if a duration/end time is configured) — no special engine support needed,
// this is just another producer of ordinary events.
func (f ScheduledFailure) Seed(q *kernel.EventQueue) {
	q.Push(kernel.Event{
		Time: f.At, Type: kernel.ResourceDegraded, Target: f.Target,
		Payload: kernel.ResourceDegradedPayload{LatencyMultiplier: f.LatencyMultiplier},
	})
}
```

The `ResourceDegraded` event's handler (in `DatabaseResource.HandleEvent`, add a case calling `ApplyFailure`) is the only kernel-level wiring needed — everything else is exactly the ordinary event flow. This directly demonstrates HLD §8's core claim: cascades need no special subsystem.

---

## 9. IR and YAML Compiler

### 9.1 IR types

```go
// internal/ir/types.go
package ir

type NodeID string

type Node struct {
	ID           NodeID
	ResourceType string            // "load_generator" | "load_balancer" | "compute" | "database"
	Parameters   map[string]any
}

type Edge struct {
	From, To NodeID
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}
```

### 9.2 YAML schema (MVP)

```yaml
# testdata/architectures/alb-ec2-rds.yaml
services:
  loadgen:
    type: load_generator # not an AWS service — the workload driver itself
  alb:
    type: aws.alb
  api:
    type: aws.ec2
    instances: 4
    workers_per_instance: 100
    service_time_mean_ms: 8 # overrides the aws.ec2 profile default, see §9.6
    service_time_stddev_ms: 3

  database:
    type: aws.rds.postgres
    max_connections: 200
    query_time_mean_ms: 6 # overrides the profile default
    query_time_stddev_ms: 2

connections:
  - from: loadgen
    to: alb
  - from: alb
    to: api
  - from: api
    to: database

workload: # a LIST of segments, chained end-to-end — this is the
  - type: constant # actual schema, not the single-object version shown
    rate: 5000 # in earlier drafts. Segment N's end_time_s must equal
    start_time_s: 0 # segment N+1's start_time_s; Validate (§9.4) should
    end_time_s: 60 # reject gaps or overlaps between segments.
  - type: ramp
    start_rate: 5000
    end_rate: 50000
    start_time_s: 60
    end_time_s: 180
  - type: constant
    rate: 50000
    start_time_s: 180
    end_time_s: 300

failures:
  - target: database
    at_s: 200
    latency_multiplier: 5.0
```

This is the actual schema needed for the §21 walkthrough's 3-phase profile — a list, each entry independently typed (`constant` or `ramp`), each with its own `[start_time_s, end_time_s)` window. `WorkloadConfig` and the segment hand-off logic are designed concretely in §14, since "the compiler chains them" needs an actual mechanism, not just an assertion that one exists.

### 9.3 Compiler

```go
// internal/ir/yaml_compiler.go
package ir

func CompileYAML(data []byte) (*Graph, WorkloadConfig, []FailureConfig, error) {
	var raw yamlSchema
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, WorkloadConfig{}, nil, fmt.Errorf("parse yaml: %w", err)
	}
	g := &Graph{}
	for name, svc := range raw.Services {
		g.Nodes = append(g.Nodes, Node{ID: NodeID(name), ResourceType: svc.Type, Parameters: svc.Parameters()})
	}
	for _, c := range raw.Connections {
		g.Edges = append(g.Edges, Edge{From: NodeID(c.From), To: NodeID(c.To)})
	}
	if err := Validate(g); err != nil {
		return nil, WorkloadConfig{}, nil, err
	}
	return g, raw.Workload.toConfig(), raw.Failures.toConfig(), nil
}
```

### 9.4 Validation

```go
// internal/ir/validate.go
package ir

func Validate(g *Graph) error {
	ids := map[NodeID]bool{}
	for _, n := range g.Nodes {
		if ids[n.ID] {
			return fmt.Errorf("duplicate node id: %s", n.ID)
		}
		ids[n.ID] = true
	}
	for _, e := range g.Edges {
		if !ids[e.From] {
			return fmt.Errorf("edge references unknown node: %s", e.From)
		}
		if !ids[e.To] {
			return fmt.Errorf("edge references unknown node: %s", e.To)
		}
	}
	if !hasExactlyOne(g, "load_generator") {
		return fmt.Errorf("architecture must have exactly one load_generator node")
	}
	if hasCycle(g) {
		return fmt.Errorf("architecture graph must not contain cycles (MVP limitation — request routing assumes a DAG)")
	}
	return nil
}
```

The "no cycles" rule is an explicit MVP constraint worth stating out loud: real architectures can have cycles (retry loops back through the same LB, service mesh call graphs), but the MVP's simple linear request lifecycle assumes a DAG. Lifting this restriction is a Phase-4-adjacent concern once retries introduce legitimate "request returns to a resource it already visited" flows — at that point cycle-handling needs to be deliberate (e.g. via hop-count limits) rather than assumed away by validation.

### 9.5 Graph → resource instantiation (the registry from HLD §4)

```go
// internal/resources/registry.go
package resources

type Constructor func(params map[string]any, deps ResourceDeps) kernel.SimResource

type ResourceDeps struct {
	World      *kernel.World
	Downstream []kernel.ResourceID // resolved from IR edges before construction
}

// registry is keyed by the GENERIC simulation class name, not by AWS service
// name — "aws.ec2" never appears as a key here. See §9.6: profile resolution
// happens one step earlier and hands this registry the generic key.
var registry = map[string]Constructor{
	"load_generator":  newLoadGeneratorFromParams,
	"load_balancer":   newLoadBalancerFromParams,
	"compute":         newComputeFromParams,
	"database":        newDatabaseFromParams,
}

func BuildWorld(g *ir.Graph) (*kernel.World, map[ir.NodeID]kernel.ResourceID, error) {
	world := &kernel.World{}
	idMap := map[ir.NodeID]kernel.ResourceID{}

	// Two-pass: register all nodes first (so downstream IDs are resolvable),
	// then construct with resolved dependencies. Necessary because edges can
	// reference nodes not yet constructed if built in a single pass.
	// Pass 1: reserve slots
	for _, n := range g.Nodes {
		idMap[n.ID] = world.Reserve()
	}
	// Pass 2: resolve each node's AWS type through the profile layer, THEN construct
	for _, n := range g.Nodes {
		resolvedType, mergedParams, err := profiles.Resolve(n.ResourceType, n.Parameters)
		if err != nil {
			return nil, nil, fmt.Errorf("node %s: %w", n.ID, err)
		}
		ctor, ok := registry[resolvedType]
		if !ok {
			return nil, nil, fmt.Errorf("profile %s resolved to unknown simulation class: %s", n.ResourceType, resolvedType)
		}
		deps := ResourceDeps{World: world, Downstream: resolveDownstream(g, n.ID, idMap)}
		r := ctor(mergedParams, deps)
		world.Set(idMap[n.ID], r)
	}
	return world, idMap, nil
}
```

`World.Reserve()`/`World.Set()` (two-phase registration, not shown fully) is needed because `ComputeResource` needs to know its downstream `DatabaseResource`'s `ResourceID` at construction time, but that ID doesn't exist until the database node is also processed — the two-pass approach breaks this chicken-and-egg problem cleanly.

The one addition to Pass 2 versus the naive version: `profiles.Resolve` sits between the IR's node type and the registry lookup. That function is the missing piece — designed fully in §9.6.

### 9.6 Service Profile Layer (the missing piece — AWS name → generic class + defaults)

This is the seam that was absent from earlier drafts of this LLD. Without it, "aws.ec2" and "aws.ecs" would each need their own registry entry and their own near-duplicate `SimResource` implementation — exactly the hard-coded-per-service trap the HLD warns against. With it, both resolve to the same `ComputeResource` class through different profiles.

```go
// internal/profiles/catalog.go
package profiles

type DisplayMeta struct {
	Icon     string // asset key the visualizer looks up, e.g. "ec2"
	Label    string // human-readable name shown on canvas, e.g. "EC2"
	Category string // grouping for a future palette UI, e.g. "compute" | "database" | "networking"
}

type ServiceProfile struct {
	AWSType      string         // "aws.ec2" — the key users/visualizer write into the IR
	SimClass     string         // "compute" — the generic registry key from §9.5
	Defaults     map[string]any // profile-level defaults, lowest-priority layer (HLD §12)
	Display      DisplayMeta
}

var catalog = map[string]ServiceProfile{
	"aws.ec2": {
		AWSType:  "aws.ec2",
		SimClass: "compute",
		Defaults: map[string]any{
			"workers_per_instance":   100,
			"service_time_mean_ms":   8.0,
			"service_time_stddev_ms": 3.0,
		},
		Display: DisplayMeta{Icon: "ec2", Label: "EC2", Category: "compute"},
	},
	"aws.ecs": {
		AWSType:  "aws.ecs",
		SimClass: "compute", // SAME class as EC2 — only defaults + display differ
		Defaults: map[string]any{
			"workers_per_instance":   50, // containers default to a smaller per-task pool
			"service_time_mean_ms":   8.0,
			"service_time_stddev_ms": 3.0,
		},
		Display: DisplayMeta{Icon: "ecs", Label: "ECS", Category: "compute"},
	},
	"aws.rds.postgres": {
		AWSType:  "aws.rds.postgres",
		SimClass: "database",
		Defaults: map[string]any{
			"max_connections":        200,
			"query_time_mean_ms":     6.0,
			"query_time_stddev_ms":   2.0,
		},
		Display: DisplayMeta{Icon: "rds", Label: "RDS PostgreSQL", Category: "database"},
	},
	"aws.alb": {
		AWSType:  "aws.alb",
		SimClass: "load_balancer",
		Defaults: map[string]any{},
		Display:  DisplayMeta{Icon: "alb", Label: "Application Load Balancer", Category: "networking"},
	},
}

// Resolve merges profile defaults under user-supplied params (user params win —
// this is layer 1→2 of the HLD §12 calibration stack: generic < AWS-derived <
// user override) and returns the generic simulation-class key for §9.5's registry.
func Resolve(awsType string, userParams map[string]any) (simClass string, merged map[string]any, err error) {
	p, ok := catalog[awsType]
	if !ok {
		return "", nil, fmt.Errorf("unknown AWS service type: %s", awsType)
	}
	merged = map[string]any{}
	for k, v := range p.Defaults {
		merged[k] = v
	}
	for k, v := range userParams { // user params override profile defaults
		merged[k] = v
	}
	return p.SimClass, merged, nil
}

// ListForVisualizer returns the flat catalog the future drag-and-drop UI reads
// to build its component palette. Deliberately exposes ONLY display metadata —
// nothing about SimClass or Defaults leaks to the frontend contract.
func ListForVisualizer() []VisualizerEntry {
	out := make([]VisualizerEntry, 0, len(catalog))
	for awsType, p := range catalog {
		out = append(out, VisualizerEntry{Type: awsType, Icon: p.Display.Icon, Label: p.Display.Label, Category: p.Display.Category})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type < out[j].Type }) // deterministic ordering
	return out
}

type VisualizerEntry struct {
	Type     string `json:"type"`
	Icon     string `json:"icon"`
	Label    string `json:"label"`
	Category string `json:"category"`
}
```

Two things this buys, concretely:

1. **Adding `aws.eks` later is a ~15-line new file in `internal/profiles/`**, not a new `SimResource` implementation — as long as its behavior is close enough to `ComputeResource` to reuse (it usually is; the difference between EC2/ECS/EKS at the level of fidelity this simulator operates is mostly "how many workers per unit," which is exactly a `Defaults` value, not new logic).
2. **`ListForVisualizer()` is the entire backend API surface the future drag-and-drop canvas needs** to render its component palette — it never imports `internal/kernel` or `internal/resources` at all, only `internal/profiles`. This is what makes "add a visualizer" a frontend-plus-thin-API project later, not a rearchitecture.

Where this plugs into the rest of the LLD: §9.3's `CompileYAML` stays unchanged (it still just reads whatever string is in `type:` — it doesn't care if that string is `"aws.ec2"` or `"compute"`); §9.4's `Validate` stays unchanged too. Only §9.5's `BuildWorld` needed the one extra call shown above. This is a small, contained change precisely because the profile layer was designed to slot into an existing seam rather than requiring one to be created.

---

## 10. Testing Strategy

This is where "prove the model works" (HLD's stated MVP goal) actually gets verified — not optional, this is the deliverable.

### 10.1 Kernel-level: closed-form queuing theory validation

```go
// internal/kernel/kernel_test.go
func TestMM1_MatchesClosedForm(t *testing.T) {
	// M/M/1 queue: arrival rate λ, service rate μ, utilization ρ = λ/μ
	// Closed form: mean wait time W = ρ / (μ - λ)  [Pollaczek-Khinchine for M/M/1]
	lambda, mu := 80.0, 100.0 // req/s
	world, engine := buildSyntheticMM1(lambda, mu, seed=42, duration=600*time.Second)
	trace := engine.Run()

	observedMeanWaitMs := trace.Metrics.Latency.Mean()
	expectedMeanWaitMs := (lambda / mu) / (mu - lambda) * 1000

	assertWithinTolerance(t, observedMeanWaitMs, expectedMeanWaitMs, 0.05) // 5% tolerance
}
```

This single test class is the most important test in the whole codebase — it's the thing that proves the event queue, service pool, and arrival generation are mathematically correct _before_ any AWS-specific complexity is layered on top where errors would be much harder to isolate. Run this for M/M/1 and M/M/c (multiple servers) at a few different utilization levels (low, moderate, near-saturation — the near-saturation case is where naive implementations most often diverge from theory due to subtle queue-management bugs).

### 10.2 Determinism regression test

```go
func TestDeterminism_SameSeedSameResult(t *testing.T) {
	run1 := runFullSimulation(seed=42)
	run2 := runFullSimulation(seed=42)
	assert.Equal(t, run1.TotalEventsProcessed, run2.TotalEventsProcessed)
	assert.Equal(t, run1.Metrics.Throughput.Series, run2.Metrics.Throughput.Series)
	assert.Equal(t, run1.Metrics.Latency.P99(), run2.Metrics.Latency.P99())
}
```

### 10.3 Statistical test for the ramp/thinning workload

```go
func TestRampWorkload_EmpiricalRateMatchesTarget(t *testing.T) {
	r := Ramp{StartRate: 1000, EndRate: 10000, StartTime: 0, EndTime: 60 * Second}
	arrivals := sampleAllArrivals(r, rng, largeN)
	for _, window := range []timeWindow{ {0,10}, {25,35}, {50,60} } {
		observedRate := countInWindow(arrivals, window) / window.Duration()
		expectedRate := r.rateAt(window.Midpoint())
		assertWithinTolerance(t, observedRate, expectedRate, 0.10) // statistical, looser tolerance
	}
}
```

### 10.4 Resource unit tests (isolated, no engine)

Each resource's `HandleEvent` is tested by directly constructing events and payloads and asserting on the returned `[]Event` slice — no `Engine` instance needed, per the design goal in §2.8. E.g. "given a `ComputeResource` with capacity 1 already occupied, a second `RequestArrived` results in zero returned events and the request sitting in `Pool.Queue`" is a 5-line test.

### 10.5 End-to-end scenario test (the §21 walkthrough, made executable)

One test that runs the full `alb-ec2-rds.yaml` architecture through the exact 3-phase traffic profile from HLD §21 and asserts: throughput plateaus in the expected RPS range, the bottleneck analyzer identifies the database connection pool as primary, and confidence/utilization numbers are directionally sane. This test is allowed looser tolerances than 10.1-10.3 (it's validating _emergent qualitative behavior_, not an exact closed-form value) but it's the test that proves the system does what the whole HLD promised.

---

## 11. CLI Wiring

```go
// cmd/infra-sim/main.go
func main() {
	root := &cobra.Command{Use: "infra-sim"}
	root.AddCommand(runCmd(), validateCmd(), reportCmd())
	root.Execute()
}

func runCmd() *cobra.Command {
	var traffic, duration, seed string
	cmd := &cobra.Command{
		Use: "run [architecture.yaml]",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, _ := os.ReadFile(args[0])
			graph, workloadCfg, failureCfg, err := ir.CompileYAML(data)
			if err != nil { return err }
			// engine.Bootstrap is the real assembly step — see §12. It replaces
			// what earlier drafts sketched as a bare kernel.NewEngine(...) call
			// that never actually had a matching constructor defined.
			eng, err := engine.Bootstrap(graph, workloadCfg, failureCfg, parseSeed(seed))
			if err != nil { return err }
			trace := eng.Run()
			// Analyze takes the Sink alongside the trace (§13) — trace alone
			// doesn't carry metrics, by design (see RunTrace's doc comment, §3).
			report := analysis.Analyze(trace, eng.Metrics)
			return render.Print(report, os.Stdout)
		},
	}
	cmd.Flags().StringVar(&traffic, "traffic", "", "override workload rate")
	cmd.Flags().StringVar(&duration, "duration", "", "override workload duration")
	cmd.Flags().StringVar(&seed, "seed", "42", "simulation seed")
	return cmd
}
```

---

## 12. Assembly — the Missing Glue (Bootstrap)

This section didn't exist in earlier drafts, and its absence was the single biggest gap: nothing previously showed how a parsed YAML file actually becomes a runnable `Engine`. Every other section assumed this wiring existed; none of them did it. `internal/engine/bootstrap.go` is new.

### 12.1 The missing config types

```go
// internal/ir/types.go (additions)
package ir

type WorkloadSegment struct {
	Type          string  // "constant" | "ramp"
	Rate          float64 // used when Type == "constant"
	StartRate     float64 // used when Type == "ramp"
	EndRate       float64 // used when Type == "ramp"
	StartTimeS    float64
	EndTimeS      float64
}

type WorkloadConfig struct {
	Segments []WorkloadSegment
}

type FailureConfig struct {
	Target            NodeID
	AtS               float64
	LatencyMultiplier float64
}
```

`Validate` (§9.4) gains one more check: consecutive segments' `EndTimeS`/`StartTimeS` must match exactly — a gap silently stalls the load generator (it produces no arrivals in the gap, which is a legitimate but probably-unintended state), and an overlap means two segments are both trying to drive the generator at once, which the single-generator design (§5.1) can't represent. Reject both.

### 12.2 CompositeGenerator — chaining workload segments

The piece that makes the §21 walkthrough's 3-phase traffic possible. Wraps an ordered list of `workload.Generator`s (one `Constant` or `Ramp` per segment) and hands off from one to the next as each reports exhaustion:

```go
// internal/workload/composite.go
package workload

type CompositeGenerator struct {
	segments []Generator
	active   int // index into segments
}

func NewComposite(segments []Generator) *CompositeGenerator {
	return &CompositeGenerator{segments: segments, active: 0}
}

func (c *CompositeGenerator) NextArrival(after kernel.SimTime, rng *rand.Rand) (kernel.SimTime, bool) {
	for c.active < len(c.segments) {
		t, ok := c.segments[c.active].NextArrival(after, rng)
		if ok {
			return t, true
		}
		// current segment exhausted (its EndTimeS reached) — advance to the
		// next one and retry from the same `after`, not from the new
		// segment's StartTimeS, so no arrival window is silently skipped
		// if segments are contiguous (which Validate now requires).
		c.active++
	}
	return 0, false // all segments exhausted, workload complete
}
```

This is a single shared RNG stream (`ctx.RNG.Workload`) across all segments, consistent with §11's determinism guarantee — switching segments doesn't reset or re-derive the stream, so the whole 3-phase run remains reproducible from one seed.

### 12.3 Bootstrap

```go
// internal/engine/bootstrap.go
package engine

func Bootstrap(g *ir.Graph, workloadCfg ir.WorkloadConfig, failureCfg []ir.FailureConfig, seed int64) (*kernel.Engine, error) {
	// 1. Build the resource graph (§9.5, which itself calls §9.6's profile resolution)
	world, idMap, err := resources.BuildWorld(g)
	if err != nil {
		return nil, fmt.Errorf("build world: %w", err)
	}

	// 2. Deterministic RNG streams (§2.7)
	rng := kernel.NewRNGStreams(seed)

	// 3. Metrics sink, initialized with one ResourceMetrics entry per resource
	//    so §6's Observe never has to nil-check a missing map entry.
	sink := metrics.NewSink(world, 1*kernel.Second) // 1s tick interval, MVP default

	// 4. Event queue, seeded with:
	//    a. the load generator's first self-tick (kicks off arrival production)
	//    b. one PolicyTick at t=0 (kicks off the metrics sampling cadence, §3.3)
	//    c. every configured failure event (§8) — this was previously defined
	//       but never actually invoked anywhere; this is the invocation site.
	queue := kernel.NewEventQueue()
	loadGenID := idMap[loadGeneratorNodeID(g)] // the one node with type: load_generator
	queue.Push(kernel.Event{Time: 0, Type: kernel.RequestArrived, Target: loadGenID})
	queue.Push(kernel.Event{Time: 0, Type: kernel.PolicyTick})
	for _, fc := range failureCfg {
		sf := failure.ScheduledFailure{
			At:                kernel.SimTime(fc.AtS * float64(kernel.Second)),
			Target:            idMap[fc.Target],
			LatencyMultiplier: fc.LatencyMultiplier,
		}
		sf.Seed(queue)
	}

	// 5. Horizon = the last workload segment's EndTimeS. Running past the
	//    point where the workload stops producing arrivals wastes wall-clock
	//    time on drain-only events with nothing new entering the system —
	//    fine to extend later with an explicit --duration override that can
	//    run longer than the workload to observe drain/recovery behavior,
	//    but the MVP default ties Horizon directly to workload length.
	lastSegment := workloadCfg.Segments[len(workloadCfg.Segments)-1]
	horizon := kernel.SimTime(lastSegment.EndTimeS * float64(kernel.Second))

	return &kernel.Engine{
		Queue:   queue,
		World:   world,
		RNG:     rng,
		Metrics: sink,
		Horizon: horizon,
	}, nil
}
```

Note what Bootstrap deliberately does NOT do: it doesn't itself construct the `CompositeGenerator` from §12.2 — that assembly happens inside `resources.BuildWorld` → `newLoadGeneratorFromParams`, which needs `workloadCfg` threaded through as an additional argument (a small, mechanical change to §9.5's `BuildWorld` signature: `BuildWorld(g *ir.Graph, workloadCfg ir.WorkloadConfig)`). Called out explicitly here because it's exactly the kind of small cross-cutting signature change that's easy to miss when different sections were designed somewhat independently.

---

## 13. Bottleneck Analyzer

The implemented analyzer evaluates the complete metric history rather than only
the first throughput plateau. For every resource it records persistent queue
frequency, maximum and final queue depth, maximum queue growth, peak utilization,
and the fraction of samples at 90% or greater utilization. A resource is a
constraint only when queueing is sustained and is supported by utilization or
queue-growth evidence; isolated queue spikes are classified as transient pressure.

`AnalyzeWithGraph` also computes graph reachability. When a downstream constraint
begins queueing no later than a constrained upstream caller, the downstream node
is ranked as the root constraint and the caller as an upstream backpressure
symptom. This is based only on graph direction and metric evidence, not AWS service
types. The topology-free `Analyze` entry point remains available for compatibility.

The report includes material served-throughput drops found by comparing adjacent
five-second windows. This exposes regime changes caused by failures even when an
earlier ramp or plateau would otherwise hide them. The fields shown in the code
sample below are retained for compatibility; the implementation additionally
returns peak/max/final evidence, score, classification, first queue time, and
throughput-drop records.

```go
// internal/analysis/bottleneck.go
package analysis

type BottleneckReport struct {
	PlateauStartTime kernel.SimTime
	PlateauThroughput float64
	Ranked            []ResourceVerdict
}

type ResourceVerdict struct {
	Resource      kernel.ResourceID
	UtilizationPct float64
	QueueDepth     float64
	IsBottleneck   bool
	Reason         string
}

func Analyze(trace kernel.RunTrace, sink *metrics.Sink) BottleneckReport {
	plateauT := detectPlateau(sink.GlobalThroughput()) // §13.1
	verdicts := []ResourceVerdict{}
	for id, rm := range sink.PerResource() {
		util := rm.Utilization.ValueAt(plateauT)
		queue := rm.QueueDepth.ValueAt(plateauT)
		queueGrowing := rm.QueueDepth.SlopeOver(plateauT, plateauT+30*kernel.Second) > 0

		v := ResourceVerdict{Resource: id, UtilizationPct: util, QueueDepth: queue}
		if queue > 0 && queueGrowing {
			v.IsBottleneck = true
			v.Reason = "queue depth positive and growing at plateau"
		} else {
			v.Reason = "no sustained queueing observed"
		}
		verdicts = append(verdicts, v)
	}
	sort.Slice(verdicts, func(i, j int) bool { return verdicts[i].UtilizationPct > verdicts[j].UtilizationPct })
	return BottleneckReport{PlateauStartTime: plateauT, Ranked: verdicts}
}
```

### 13.1 Plateau detection

```go
func detectPlateau(throughput *metrics.TimeSeries) kernel.SimTime {
	// Find the earliest time window where the second derivative of served
	// throughput flattens (rate of increase drops below a threshold) while
	// offered load (from workload config) is still increasing.
	// MVP: simple approach — compare throughput growth over consecutive
	// 10s windows; flag plateau when growth rate drops below 5% of the
	// growth rate in the first window of the ramp phase.
	windows := throughput.Windows(10 * kernel.Second)
	baselineGrowth := windows[1].Value - windows[0].Value
	for i := 2; i < len(windows); i++ {
		growth := windows[i].Value - windows[i-1].Value
		if growth < 0.05*baselineGrowth {
			return windows[i].Start
		}
	}
	return 0 // no plateau detected in this run
}
```

This is explicitly the _simplest correct-enough_ version — flagged in HLD §7 as needing the causal-tracing refinement once retries exist (Phase 4). MVP doesn't need it because MVP has no retries, so "highest utilization among resources with a growing queue" is already non-naive enough to avoid the specific failure mode HLD §7 called out (CPU-busy-but-not-actually-limiting still gets correctly excluded here, since it won't show `queueGrowing == true`).

---

## 14. Visualizer and Web API

The initial visual canvas is implemented in `web/` using React, TypeScript, Vite, and React Flow. The Go server in `cmd/infra-sim-web` serves the production bundle and routes `/api` requests through `internal/webapi`.

The API surface is intentionally small:

- `GET /api/catalog` returns service display metadata, safe editable defaults, and configuration-field schemas.
- `POST /api/architectures/import-yaml` compiles an uploaded YAML document and returns its canonical graph, workload segments, and failures for editor hydration.
- `POST /api/validate` validates graph structure, workload continuity, resource profiles, parameters, and MVP topology.
- `POST /api/simulations` creates a bounded asynchronous job and returns its ID plus the numeric-resource-to-node mapping.
- `GET /api/simulations/{id}` returns job state, virtual-time progress, event/request counts, live resource pressure snapshots, and the final result when complete.
- `DELETE /api/simulations/{id}` requests cancellation. The kernel checks cancellation between bounded event batches.
- `POST /api/simulations/run` remains as a compatibility endpoint but uses the same concurrency, wall-clock, event-count, and queue-depth safeguards.

The editor uses the profile catalog rather than hard-coding backend behavior. Simulation-class names remain private; the UI only sees AWS type names, display metadata, safe defaults, and fields it may edit.

The editor starts with a blank canvas. Users either construct the graph manually from the catalog or select a `.yaml`/`.yml` file; YAML parsing remains server-side so the CLI and browser share one compiler and validation contract. Imported DAGs are laid out by dependency depth in the browser.

The visualizer flow is:

1. Frontend calls a thin API wrapping `profiles.ListForVisualizer()` → renders palette of draggable AWS service icons, grouped by `Category`.
2. User drags an "EC2" icon onto canvas, draws a connection to an "RDS" icon → frontend serializes this as IR (`type: aws.ec2`, an edge to `type: aws.rds.postgres`) — same YAML-equivalent shape as §9.2, just constructed visually instead of hand-written.
3. Submitted to the same `CompileYAML`-equivalent path (a JSON variant of it) → same `Validate` → same `BuildWorld` → same kernel, completely unaware anything changed.

No resource-class changes were needed to add the visualizer. Vite proxies `/api` to Go during development; production builds into `web/dist`, which the Go web command serves with SPA fallback routing. Project persistence, authentication, collaboration, export, durable job history, and distributed workers remain later concerns.

Web jobs are limited to two concurrent runs, eight million events, 250,000 queued requests, and two wall-clock minutes. These limits prevent high-rate scenarios from exhausting process memory. At each virtual-second policy tick, the kernel publishes a compact snapshot rather than streaming individual request events; the UI uses it to animate active edges and color nodes green, yellow, or red based on utilization and queue pressure. This preserves simulation performance while still making system behavior visible.

Before a web run, the API analytically estimates offered arrivals across constant and ramp segments. Runs above 200,000 estimated arrivals use a deterministic sampling factor `ceil(estimated_arrivals / 200000)`. Arrival rates and resource capacities are divided by that factor; each representative `RequestState` carries the factor as its weight. Throughput, completion, rejection, and queue metrics multiply sampled observations by this weight, while latency histograms treat the observation as a weighted sample. The UI reports the factor, and exact CLI behavior remains unchanged at factor 1. Safety queue limits count stored representative objects rather than weighted queue depth so they protect actual memory consumption.

---

## 15. What's Deliberately Not Designed Here

Per HLD §18 MVP scope, these have no LLD yet because building them now would be premature — noting so nothing looks like an oversight:

- Autoscaling event handlers (no `ScalingPolicy` type yet)
- Retry/backoff logic on `ComputeResource`/`DatabaseResource`
- Kafka/SQS/Redis resource implementations
- Terraform/CFN importers
- Cost model
- Any persistence beyond flat JSON result files (`report.Print` writing to disk is a 10-line addition when needed, not designed in detail here)

Each slots into the registry (§9.5) and `SimResource` interface (§2.5) without kernel changes when the time comes — that's the extensibility bet this whole LLD is making, and the point where it'd be validated is Phase 3 (HLD roadmap) when Kafka gets added and, if this design is right, requires zero kernel diffs.
