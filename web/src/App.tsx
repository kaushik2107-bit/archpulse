import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Background,
  BackgroundVariant,
  Controls,
  MiniMap,
  ReactFlow,
  addEdge,
  useEdgesState,
  useNodesState,
  type Connection,
  type NodeTypes,
} from '@xyflow/react'
import {
  Activity,
  AlertTriangle,
  Check,
  ChevronRight,
  Clock3,
  Pause,
  Play,
  Plus,
  RotateCcw,
  Settings2,
  Trash2,
  Upload,
  Workflow,
  X,
  Zap,
} from 'lucide-react'
import { cancelSimulation, getCatalog, getSimulation, importYAML, startSimulation, validateArchitecture } from './api'
import MetricChart from './MetricChart'
import ServiceNodeCard from './ServiceNode'
import type {
  ArchitectureEdge,
  CatalogEntry,
  FailureConfig,
  MetricPoint,
  RunResponse,
  ServiceNode,
  SimulationJob,
  SimulationPayload,
  WorkloadSegment,
} from './types'

const nodeTypes: NodeTypes = { service: ServiceNodeCard }

const defaultWorkload: WorkloadSegment[] = [
  { type: 'constant', rate: 1_000, start_time_s: 0, end_time_s: 30 },
]

type Status = { kind: 'success' | 'error' | 'info'; message: string } | null

export default function App() {
  const [nodes, setNodes, onNodesChange] = useNodesState<ServiceNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<ArchitectureEdge>([])
  const [catalog, setCatalog] = useState<CatalogEntry[]>([])
  const [workload, setWorkload] = useState<WorkloadSegment[]>(defaultWorkload)
  const [failures, setFailures] = useState<FailureConfig[]>([])
  const [seed, setSeed] = useState(42)
  const [status, setStatus] = useState<Status>({ kind: 'info', message: 'Loading service catalog…' })
  const [running, setRunning] = useState(false)
  const [activeJob, setActiveJob] = useState<string | null>(null)
  const [jobProgress, setJobProgress] = useState<SimulationJob['progress'] | null>(null)
  const [result, setResult] = useState<RunResponse | null>(null)
  const [playbackTime, setPlaybackTime] = useState(0)
  const [playbackSpeed, setPlaybackSpeed] = useState(10)
  const [playing, setPlaying] = useState(false)
  const [bottomTab, setBottomTab] = useState<'workload' | 'failures' | 'results'>('workload')
  const nextID = useRef(1)
  const fileInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    getCatalog()
      .then((services) => {
        setCatalog(services)
        setStatus({ kind: 'success', message: 'Simulator connected' })
      })
      .catch((error: Error) => setStatus({ kind: 'error', message: error.message }))
  }, [])

  useEffect(() => {
    if (!activeJob) return
    let disposed = false
    let timer: number | undefined
    const poll = async () => {
      try {
        const job = await getSimulation(activeJob)
        if (disposed) return
        setJobProgress(job.progress)
        applyPressure(job)
        if (job.status === 'completed' && job.result) {
          setResult({ result: job.result, resources: job.resources })
          setPlaybackTime(0)
          setPlaying(true)
          setBottomTab('results')
          setRunning(false)
          setActiveJob(null)
          setStatus({ kind: 'success', message: `Simulation complete — ${job.result.latency.count.toLocaleString()} requests served` })
          return
        }
        if (job.status === 'failed' || job.status === 'cancelled') {
          setRunning(false)
          setActiveJob(null)
          setStatus({ kind: job.status === 'failed' ? 'error' : 'info', message: job.error ?? `Simulation ${job.status}` })
          return
        }
        const virtualSeconds = job.progress.virtual_time_ns / 1e9
        const horizonSeconds = job.progress.horizon_ns / 1e9
        setStatus({ kind: 'info', message: `${job.status === 'queued' ? 'Queued' : 'Running'} · ${job.progress.percent.toFixed(0)}% · ${virtualSeconds.toFixed(0)}s / ${horizonSeconds.toFixed(0)}s virtual time` })
        timer = window.setTimeout(poll, 500)
      } catch (error) {
        if (disposed) return
        setRunning(false)
        setActiveJob(null)
        setStatus({ kind: 'error', message: (error as Error).message })
      }
    }
    void poll()
    return () => { disposed = true; if (timer !== undefined) window.clearTimeout(timer) }
  }, [activeJob])

  const playbackDuration = (result?.result.trace.FinalTime ?? 0) / 1e9

  useEffect(() => {
    if (!playing || !result || playbackDuration <= 0) return
    const timer = window.setInterval(() => {
      setPlaybackTime((current) => {
        const next = Math.min(playbackDuration, current + playbackSpeed / 10)
        if (next >= playbackDuration) setPlaying(false)
        return next
      })
    }, 100)
    return () => window.clearInterval(timer)
  }, [playing, playbackDuration, playbackSpeed, result])

  useEffect(() => {
    if (!result || running) return
    applyTimelinePressure(result, playbackTime)
  }, [result, running, playbackTime])

  const onConnect = useCallback((connection: Connection) => {
    if (connection.source === connection.target) return
    setEdges((current) => addEdge({ ...connection, animated: false }, current))
  }, [setEdges])

  const selectedNode = nodes.find((node) => node.selected)
  const selectedCatalog = catalog.find((entry) => entry.type === selectedNode?.data.resourceType)

  const payload = useMemo<SimulationPayload>(() => ({
    graph: {
      nodes: nodes.map((node) => ({ id: node.id, resource_type: node.data.resourceType, parameters: node.data.parameters })),
      edges: edges.map((edge) => ({ from: edge.source, to: edge.target })),
    },
    workload: { segments: workload },
    failures,
  }), [nodes, edges, workload, failures])

  function addService(service: CatalogEntry) {
    const id = `${service.icon}-${nextID.current++}`
    setNodes((current) => current.concat({
      id,
      type: 'service',
      position: { x: 180 + (current.length % 3) * 220, y: 90 + Math.floor(current.length / 3) * 130 },
      data: { label: service.label, resourceType: service.type, category: service.category, icon: service.icon, parameters: { ...service.defaults } },
    }))
    setStatus({ kind: 'info', message: `${service.label} added — connect it using the node handles` })
  }

  function updateParameter(key: string, value: number) {
    if (!selectedNode) return
    setNodes((current) => current.map((node) => node.id === selectedNode.id
      ? { ...node, data: { ...node.data, parameters: { ...node.data.parameters, [key]: value } } }
      : node))
  }

  function removeSelectedNode() {
    if (!selectedNode) return
    setNodes((current) => current.filter((node) => node.id !== selectedNode.id))
    setEdges((current) => current.filter((edge) => edge.source !== selectedNode.id && edge.target !== selectedNode.id))
    setFailures((current) => current.filter((failure) => failure.target !== selectedNode.id))
  }

  function clearArchitecture() {
    setNodes([])
    setEdges([])
    setWorkload(defaultWorkload.map((segment) => ({ ...segment })))
    setFailures([])
    setResult(null)
    setPlaying(false)
    setPlaybackTime(0)
    setStatus({ kind: 'info', message: 'Canvas cleared — add nodes or load a YAML architecture' })
  }

  async function loadYAML(file: File) {
    setStatus({ kind: 'info', message: `Loading ${file.name}…` })
    try {
      const services = catalog.length > 0 ? catalog : await getCatalog()
      if (catalog.length === 0) setCatalog(services)
      const imported = await importYAML(await file.text())
      setNodes(layoutImportedNodes(imported.graph.nodes, imported.graph.edges, services))
      setEdges(imported.graph.edges.map((edge, index) => ({ id: `yaml-${index}-${edge.from}-${edge.to}`, source: edge.from, target: edge.to, animated: false })))
      setWorkload(imported.workload.segments)
      setFailures(imported.failures ?? [])
      setResult(null)
      setPlaying(false)
      setPlaybackTime(0)
      setStatus({ kind: 'success', message: `${file.name} loaded — ${imported.graph.nodes.length} nodes and ${imported.graph.edges.length} connections` })
    } catch (error) {
      setStatus({ kind: 'error', message: (error as Error).message })
    }
  }

  async function validate() {
    setStatus({ kind: 'info', message: 'Validating architecture…' })
    try {
      await validateArchitecture(payload)
      setStatus({ kind: 'success', message: 'Architecture is valid and ready to simulate' })
    } catch (error) {
      setStatus({ kind: 'error', message: (error as Error).message })
    }
  }

  async function simulate() {
    setRunning(true)
    setResult(null)
    setPlaying(false)
    setPlaybackTime(0)
    setJobProgress(null)
    setNodes((current) => current.map((node) => ({ ...node, data: { ...node.data, pressure: 'active', liveUtilization: 0, liveQueueDepth: 0 } })))
    setStatus({ kind: 'info', message: 'Simulation running…' })
    try {
      const job = await startSimulation(payload, seed)
      setActiveJob(job.simulation_id)
    } catch (error) {
      setStatus({ kind: 'error', message: (error as Error).message })
      setRunning(false)
    }
  }

  async function cancelRun() {
    if (!activeJob) return
    setStatus({ kind: 'info', message: 'Cancelling simulation…' })
    try {
      await cancelSimulation(activeJob)
    } catch (error) {
      setStatus({ kind: 'error', message: (error as Error).message })
    }
  }

  function applyPressure(job: SimulationJob) {
    const referenceByResource = new Map(job.resources.map((resource) => [resource.resource_id, resource.node_id]))
    const pressureByNode = new Map(job.progress.resources.map((resource) => {
      const nodeID = referenceByResource.get(resource.resource_id) ?? ''
      const pressure = classifyPressure(resource.utilization_pct, resource.queue_depth, resource.in_flight > 0)
      return [nodeID, { pressure, utilization: resource.utilization_pct, queue: resource.queue_depth }] as const
    }))
    setNodes((current) => current.map((node) => {
      const live = pressureByNode.get(node.id)
      return live ? { ...node, data: { ...node.data, pressure: live.pressure, liveUtilization: live.utilization, liveQueueDepth: live.queue } } : node
    }))
  }

  function applyTimelinePressure(completed: RunResponse, atSeconds: number) {
    const at = atSeconds * 1e9
    const trafficActive = valueAt(completed.result.throughput_rps, at) > 0
    const nodeByResource = new Map(completed.resources.map((resource) => [resource.resource_id, resource.node_id]))
    const pressureByNode = new Map((completed.result.resource_timelines ?? []).map((timeline) => {
      // Resource snapshots are instantaneous and can alternate busy/idle for
      // short services. A trailing virtual-time window prevents visual flicker.
      const utilization = maxValueInWindow(timeline.utilization_pct, at, 5e9)
      const queue = maxValueInWindow(timeline.queue_depth, at, 5e9)
      const pressure = classifyPressure(utilization, queue, trafficActive)
      return [nodeByResource.get(timeline.resource_id) ?? '', { pressure, utilization, queue }] as const
    }))
    setNodes((current) => current.map((node) => {
      const frame = pressureByNode.get(node.id)
      return frame ? { ...node, data: { ...node.data, pressure: frame.pressure, liveUtilization: frame.utilization, liveQueueDepth: frame.queue } } : node
    }))
  }

  const groups = useMemo(() => catalog.reduce<Record<string, CatalogEntry[]>>((all, service) => {
    ;(all[service.category] ??= []).push(service)
    return all
  }, {}), [catalog])

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand"><div className="brand-mark"><Workflow size={21} /></div><div><strong>Infra-Sim</strong><span>Architecture Studio</span></div></div>
        <div className={`connection-status ${status?.kind ?? 'info'}`}>
          <span className="status-dot" />{status?.message ?? 'Ready'}
        </div>
        <div className="topbar-actions">
          <label className="seed-control">Seed <input type="number" value={seed} onChange={(event) => setSeed(Number(event.target.value))} /></label>
          <button className="button ghost" onClick={validate}><Check size={16} /> Validate</button>
          <button className={`button primary ${running ? 'cancel-run' : ''}`} onClick={running ? cancelRun : simulate}>{running ? <X size={16} /> : <Play size={16} fill="currentColor" />}{running ? 'Cancel run' : 'Run simulation'}</button>
        </div>
      </header>

      <main className="workspace">
        <aside className="palette panel">
          <div className="panel-heading"><div><span className="eyebrow">Components</span><h2>Service palette</h2></div></div>
          <p className="panel-help">Start from a YAML file or add services and connect them manually.</p>
          <div className="import-box">
            <input ref={fileInput} type="file" accept=".yaml,.yml,application/yaml,text/yaml" hidden onChange={(event) => { const file = event.target.files?.[0]; if (file) void loadYAML(file); event.target.value = '' }} />
            <button className="button import" onClick={() => fileInput.current?.click()} disabled={running}><Upload size={15} /> Load YAML file</button>
          </div>
          <div className="palette-groups">
            {Object.entries(groups).map(([category, services]) => (
              <section key={category}><h3>{category}</h3>{services.map((service) => (
                <button className="palette-item" key={service.type} onClick={() => addService(service)} disabled={running}>
                  <span className={`palette-icon category-${category}`}><Zap size={17} /></span>
                  <span><strong>{service.label}</strong><small>{service.type}</small></span><Plus size={15} />
                </button>
              ))}</section>
            ))}
          </div>
          <button className="button reset" onClick={clearArchitecture} disabled={running}><Trash2 size={15} /> Clear canvas</button>
        </aside>

        <section className={`canvas-wrap ${running || playing ? 'simulation-active' : ''}`}>
          <div className="canvas-heading"><div><span className="eyebrow">Architecture</span><strong>{nodes.length} nodes · {edges.length} connections</strong></div><span className="canvas-tip">Select a node to configure it</span></div>
          <ReactFlow
            nodes={nodes}
            edges={edges.map((edge) => ({ ...edge, animated: running || playing }))}
            proOptions={{ hideAttribution: true }}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodeTypes={nodeTypes}
            fitView
            nodesDraggable={!running}
            nodesConnectable={!running}
            minZoom={0.35}
            maxZoom={1.6}
            defaultEdgeOptions={{ style: { stroke: '#6f8095', strokeWidth: 2 } }}
          >
            <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="#293747" />
            <Controls showInteractive={false} />
            <MiniMap
              pannable
              zoomable
              ariaLabel="Architecture overview"
              bgColor="#0a1119"
              maskColor="rgba(3, 8, 13, 0.72)"
              maskStrokeColor="#3a4a5e"
              maskStrokeWidth={1}
              nodeStrokeColor="#172333"
              nodeStrokeWidth={2}
              nodeColor={(node) => node.data.category === 'database' ? '#2d79c7' : node.data.category === 'compute' ? '#9b62d1' : '#e39018'}
              style={{ width: 155, height: 96 }}
            />
          </ReactFlow>
          {nodes.length === 0 && <div className="canvas-empty"><div className="canvas-empty__icon"><Workflow size={28} /></div><strong>Build your first architecture</strong><p>Add services from the palette and connect them, or load an existing Infra-Sim YAML file.</p><button className="button primary" onClick={() => fileInput.current?.click()}><Upload size={15} /> Choose YAML file</button></div>}
          {running && jobProgress && <div className="run-progress"><div className="run-progress__top"><span>Simulating virtual traffic {jobProgress.sampling_factor > 1 && <b>· {jobProgress.sampling_factor}× weighted sample</b>}</span><strong>{jobProgress.percent.toFixed(0)}%</strong></div><div className="progress-track"><span style={{ width: `${Math.max(1, jobProgress.percent)}%` }} /></div><div className="run-progress__meta"><span>{(jobProgress.completed_requests ?? 0).toLocaleString()} represented requests served</span><span>{jobProgress.queued_requests.toLocaleString()} queued</span><span>{jobProgress.events_processed.toLocaleString()} simulated events</span></div></div>}
          {!running && result && <div className="playback-controls">
            <PlaybackRequestGraph points={result.result.throughput_rps ?? []} currentTime={playbackTime * 1e9} duration={result.result.trace.FinalTime} />
            <button className="playback-button" aria-label={playing ? 'Pause playback' : 'Play simulation'} onClick={() => { if (playbackTime >= playbackDuration) setPlaybackTime(0); setPlaying((current) => !current) }}>{playing ? <Pause size={17} fill="currentColor" /> : <Play size={17} fill="currentColor" />}</button>
            <button className="playback-button secondary" aria-label="Restart playback" onClick={() => { setPlaybackTime(0); setPlaying(true) }}><RotateCcw size={16} /></button>
            <strong>{formatPlaybackTime(playbackTime)}</strong>
            <input aria-label="Simulation playback position" type="range" min="0" max={playbackDuration} step="0.1" value={Math.min(playbackTime, playbackDuration)} onChange={(event) => { setPlaying(false); setPlaybackTime(Number(event.target.value)) }} />
            <span>{formatPlaybackTime(playbackDuration)}</span>
            <select aria-label="Playback speed" value={playbackSpeed} onChange={(event) => setPlaybackSpeed(Number(event.target.value))}><option value="1">1×</option><option value="5">5×</option><option value="10">10×</option><option value="30">30×</option><option value="60">60×</option></select>
          </div>}
        </section>

        <aside className="inspector panel">
          <div className="panel-heading"><div><span className="eyebrow">Properties</span><h2>Configuration</h2></div><Settings2 size={18} /></div>
          {selectedNode ? (
            <div className="inspector-content">
              <div className="selected-summary"><div className={`selected-icon category-${selectedNode.data.category}`}><Activity size={20} /></div><div><strong>{selectedNode.data.label}</strong><span>{selectedNode.id}</span></div></div>
              {(selectedCatalog?.parameters?.length ?? 0) > 0 ? <div className="field-list">
                {selectedCatalog!.parameters!.map((field) => (
                  <label className="form-field" key={field.key}><span>{field.label}{field.unit && <small>{field.unit}</small>}</span><input type="number" min={field.min} step={field.step} value={Number(selectedNode.data.parameters[field.key] ?? 0)} onChange={(event) => updateParameter(field.key, Number(event.target.value))} /></label>
                ))}
              </div> : <div className="empty-compact">This node has no configurable resource parameters in the MVP.</div>}
              <button className="button danger" onClick={removeSelectedNode}><Trash2 size={15} /> Remove node</button>
            </div>
          ) : <div className="empty-state"><div><Settings2 size={26} /></div><strong>No node selected</strong><p>Choose a node on the canvas to inspect and tune its capacity or latency.</p></div>}
        </aside>
      </main>

      <section className="bottom-panel">
        <nav className="tabs">
          <button className={bottomTab === 'workload' ? 'active' : ''} onClick={() => setBottomTab('workload')}><Activity size={15} /> Workload <span>{workload.length}</span></button>
          <button className={bottomTab === 'failures' ? 'active' : ''} onClick={() => setBottomTab('failures')}><AlertTriangle size={15} /> Failures <span>{failures.length}</span></button>
          <button className={bottomTab === 'results' ? 'active' : ''} onClick={() => setBottomTab('results')}><Zap size={15} /> Results {result && <span className="ready-pill">ready</span>}</button>
        </nav>
        <div className="tab-content">
          {bottomTab === 'workload' && <WorkloadEditor segments={workload} onChange={setWorkload} />}
          {bottomTab === 'failures' && <FailureEditor failures={failures} nodes={nodes} onChange={setFailures} />}
          {bottomTab === 'results' && <Results result={result} samplingFactor={jobProgress?.sampling_factor ?? 1} />}
        </div>
      </section>
    </div>
  )
}

function WorkloadEditor({ segments, onChange }: { segments: WorkloadSegment[]; onChange: (segments: WorkloadSegment[]) => void }) {
  function update(index: number, patch: Partial<WorkloadSegment>) {
    onChange(segments.map((segment, current) => current === index ? { ...segment, ...patch } : segment))
  }
  function add() {
    const start = segments.at(-1)?.end_time_s ?? 0
    onChange(segments.concat({ type: 'constant', rate: 1_000, start_time_s: start, end_time_s: start + 10 }))
  }
  return <div className="editor-section">
    <div className="editor-intro"><div><span className="eyebrow">Traffic timeline</span><h3>Arrival phases</h3></div><button className="button ghost" onClick={add}><Plus size={15} /> Add phase</button></div>
    <div className="segment-list">{segments.map((segment, index) => <div className="segment-card" key={index}>
      <div className="segment-number">{index + 1}</div>
      <label>Pattern<select value={segment.type} onChange={(event) => update(index, event.target.value === 'ramp' ? { type: 'ramp', start_rate: segment.rate ?? 1_000, end_rate: (segment.rate ?? 1_000) * 2 } : { type: 'constant', rate: segment.end_rate ?? 1_000 })}><option value="constant">Constant</option><option value="ramp">Ramp</option></select></label>
      {segment.type === 'constant' ? <label>Rate <span>RPS</span><input type="number" min="1" value={segment.rate ?? 0} onChange={(event) => update(index, { rate: Number(event.target.value) })} /></label> : <><label>Start <span>RPS</span><input type="number" min="1" value={segment.start_rate ?? 0} onChange={(event) => update(index, { start_rate: Number(event.target.value) })} /></label><ChevronRight className="phase-arrow" size={16} /><label>End <span>RPS</span><input type="number" min="1" value={segment.end_rate ?? 0} onChange={(event) => update(index, { end_rate: Number(event.target.value) })} /></label></>}
      <label>From <span>sec</span><input type="number" min="0" value={segment.start_time_s} onChange={(event) => update(index, { start_time_s: Number(event.target.value) })} /></label>
      <label>Until <span>sec</span><input type="number" min="0" value={segment.end_time_s} onChange={(event) => update(index, { end_time_s: Number(event.target.value) })} /></label>
      <button className="icon-button" aria-label="Remove phase" onClick={() => onChange(segments.filter((_, current) => current !== index))}><X size={16} /></button>
    </div>)}</div>
  </div>
}

function FailureEditor({ failures, nodes, onChange }: { failures: FailureConfig[]; nodes: ServiceNode[]; onChange: (failures: FailureConfig[]) => void }) {
  function update(index: number, patch: Partial<FailureConfig>) { onChange(failures.map((failure, current) => current === index ? { ...failure, ...patch } : failure)) }
  function add() { const target = nodes.find((node) => node.data.category === 'database')?.id ?? nodes[0]?.id ?? ''; onChange(failures.concat({ target, at_s: 10, latency_multiplier: 2 })) }
  return <div className="editor-section">
    <div className="editor-intro"><div><span className="eyebrow">Chaos controls</span><h3>Scheduled degradation</h3></div><button className="button ghost" onClick={add}><Plus size={15} /> Add failure</button></div>
    {failures.length === 0 ? <div className="wide-empty">No failures configured. The simulation will run under normal conditions.</div> : <div className="failure-list">{failures.map((failure, index) => <div className="failure-card" key={index}>
      <AlertTriangle size={18} />
      <label>Target<select value={failure.target} onChange={(event) => update(index, { target: event.target.value })}>{nodes.map((node) => <option key={node.id} value={node.id}>{node.data.label} · {node.id}</option>)}</select></label>
      <label>At <span>sec</span><input type="number" min="0" value={failure.at_s} onChange={(event) => update(index, { at_s: Number(event.target.value) })} /></label>
      <label>Latency multiplier <span>×</span><input type="number" min="1" step="0.5" value={failure.latency_multiplier} onChange={(event) => update(index, { latency_multiplier: Number(event.target.value) })} /></label>
      <button className="icon-button" aria-label="Remove failure" onClick={() => onChange(failures.filter((_, current) => current !== index))}><X size={16} /></button>
    </div>)}</div>}
  </div>
}

function Results({ result, samplingFactor }: { result: RunResponse | null; samplingFactor: number }) {
  if (!result) return <div className="results-empty"><Play size={24} /><div><strong>No simulation results yet</strong><p>Validate the graph, then run the simulation to inspect throughput, latency, and bottlenecks.</p></div></div>
  const data = result.result
  const primary = data.bottleneck.ranked_resources?.find((item) => item.is_bottleneck)
  const resource = result.resources.find((item) => item.resource_id === primary?.resource_id)
  return <div className="results-grid">
    <div className="metric-cards">
      <Metric label="Requests served" value={data.latency.count.toLocaleString()} detail={`${data.trace.TotalEventsProcessed.toLocaleString()} events${samplingFactor > 1 ? ` · ${samplingFactor}× sample` : ''}`} />
      <Metric label="p95 latency" value={formatLatency(data.latency.p95_us)} detail={`p50 ${formatLatency(data.latency.p50_us)}`} />
      <Metric label="p99 latency" value={formatLatency(data.latency.p99_us)} detail={`mean ${formatLatency(data.latency.mean_us)}`} />
      <Metric label="Primary bottleneck" value={resource?.node_id ?? 'None detected'} detail={primary ? primary.reason : 'No sustained capacity constraint detected'} warning={Boolean(primary)} />
    </div>
    <div className="chart-card"><div className="chart-heading"><div><span className="eyebrow">Served throughput</span><h3>Requests per second</h3></div><div className="chart-meta"><Clock3 size={14} /> {(data.trace.FinalTime / 1e9).toFixed(0)}s virtual time</div></div><MetricChart points={data.throughput_rps ?? []} /></div>
  </div>
}

function Metric({ label, value, detail, warning = false }: { label: string; value: string; detail: string; warning?: boolean }) {
  return <div className={`metric-card ${warning ? 'warning' : ''}`}><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>
}

function formatLatency(microseconds: number) {
  return microseconds >= 1_000 ? `${(microseconds / 1_000).toFixed(1)} ms` : `${microseconds.toFixed(0)} µs`
}

function formatPlaybackTime(seconds: number) {
  const minutes = Math.floor(seconds / 60)
  const remainder = Math.floor(seconds % 60)
  return `${minutes}:${remainder.toString().padStart(2, '0')}`
}

function valueAt(points: MetricPoint[] | null | undefined, at: number) {
  if (!points?.length) return 0
  let low = 0
  let high = points.length
  while (low < high) {
    const middle = Math.floor((low + high) / 2)
    if (points[middle].time_ns <= at) low = middle + 1
    else high = middle
  }
  return low === 0 ? 0 : points[low - 1].value
}

function classifyPressure(utilization: number, queue: number, active: boolean): 'idle' | 'active' | 'warning' | 'critical' {
  if (queue > 0 || utilization >= 85) return 'critical'
  if (utilization >= 50) return 'warning'
  if (active || utilization > 0) return 'active'
  return 'idle'
}

function maxValueInWindow(points: MetricPoint[] | null | undefined, at: number, window: number) {
  if (!points?.length) return 0
  const start = Math.max(0, at - window)
  let maximum = valueAt(points, at)
  for (let index = points.length - 1; index >= 0; index--) {
    const point = points[index]
    if (point.time_ns > at) continue
    if (point.time_ns < start) break
    maximum = Math.max(maximum, point.value)
  }
  return maximum
}

function PlaybackRequestGraph({ points, currentTime, duration }: { points: MetricPoint[]; currentTime: number; duration: number }) {
  const width = 620
  const height = 58
  const peak = Math.max(1, ...points.map((point) => point.value))
  const line = points.map((point, index) => {
    const x = duration > 0 ? (point.time_ns / duration) * width : 0
    const y = height - 5 - (point.value / peak) * (height - 12)
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`
  }).join(' ')
  const playhead = duration > 0 ? Math.min(width, Math.max(0, currentTime / duration * width)) : 0
  const currentRPS = valueAt(points, currentTime)
  return <div className="playback-graph">
    <div><span>Served requests</span><strong>{Math.round(currentRPS).toLocaleString()} RPS</strong></div>
    <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" role="img" aria-label="Served requests per second during playback">
      <path className="playback-graph__area" d={`${line} L ${width} ${height} L 0 ${height} Z`} />
      <path className="playback-graph__line" d={line} />
      <line className="playback-graph__playhead" x1={playhead} x2={playhead} y1="0" y2={height} />
      <circle className="playback-graph__point" cx={playhead} cy={height - 5 - (currentRPS / peak) * (height - 12)} r="3" />
    </svg>
  </div>
}

function layoutImportedNodes(
  graphNodes: SimulationPayload['graph']['nodes'],
  graphEdges: SimulationPayload['graph']['edges'],
  catalog: CatalogEntry[],
): ServiceNode[] {
  const indegree = new Map(graphNodes.map((node) => [node.id, 0]))
  const outgoing = new Map(graphNodes.map((node) => [node.id, [] as string[]]))
  graphEdges.forEach((edge) => {
    indegree.set(edge.to, (indegree.get(edge.to) ?? 0) + 1)
    outgoing.get(edge.from)?.push(edge.to)
  })
  const levels = new Map<string, number>()
  const queue = graphNodes.filter((node) => indegree.get(node.id) === 0).map((node) => node.id)
  queue.forEach((id) => levels.set(id, 0))
  while (queue.length > 0) {
    const id = queue.shift()!
    for (const target of outgoing.get(id) ?? []) {
      levels.set(target, Math.max(levels.get(target) ?? 0, (levels.get(id) ?? 0) + 1))
      indegree.set(target, (indegree.get(target) ?? 1) - 1)
      if (indegree.get(target) === 0) queue.push(target)
    }
  }
  const rowByLevel = new Map<number, number>()
  return graphNodes.map((node) => {
    const service = catalog.find((entry) => entry.type === node.resource_type)
    const level = levels.get(node.id) ?? 0
    const row = rowByLevel.get(level) ?? 0
    rowByLevel.set(level, row + 1)
    return {
      id: node.id,
      type: 'service',
      position: { x: 45 + level * 250, y: 95 + row * 115 },
      data: {
        label: service?.label ?? node.resource_type,
        resourceType: node.resource_type,
        category: service?.category ?? 'unknown',
        icon: service?.icon ?? 'server',
        parameters: { ...(service?.defaults ?? {}), ...node.parameters },
      },
    }
  })
}
