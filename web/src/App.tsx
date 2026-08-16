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
  Play,
  Plus,
  Settings2,
  Trash2,
  Upload,
  Workflow,
  X,
  Zap,
} from 'lucide-react'
import { getCatalog, importYAML, runSimulation, validateArchitecture } from './api'
import MetricChart from './MetricChart'
import ServiceNodeCard from './ServiceNode'
import type {
  ArchitectureEdge,
  CatalogEntry,
  FailureConfig,
  RunResponse,
  ServiceNode,
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
  const [result, setResult] = useState<RunResponse | null>(null)
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

  const onConnect = useCallback((connection: Connection) => {
    if (connection.source === connection.target) return
    setEdges((current) => addEdge({ ...connection, animated: true }, current))
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
    setStatus({ kind: 'info', message: 'Canvas cleared — add nodes or load a YAML architecture' })
  }

  async function loadYAML(file: File) {
    setStatus({ kind: 'info', message: `Loading ${file.name}…` })
    try {
      const services = catalog.length > 0 ? catalog : await getCatalog()
      if (catalog.length === 0) setCatalog(services)
      const imported = await importYAML(await file.text())
      setNodes(layoutImportedNodes(imported.graph.nodes, imported.graph.edges, services))
      setEdges(imported.graph.edges.map((edge, index) => ({ id: `yaml-${index}-${edge.from}-${edge.to}`, source: edge.from, target: edge.to, animated: true })))
      setWorkload(imported.workload.segments)
      setFailures(imported.failures ?? [])
      setResult(null)
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
    setStatus({ kind: 'info', message: 'Simulation running…' })
    try {
      const response = await runSimulation(payload, seed)
      setResult(response)
      setBottomTab('results')
      setStatus({ kind: 'success', message: `Simulation complete — ${response.result.latency.count.toLocaleString()} requests served` })
    } catch (error) {
      setStatus({ kind: 'error', message: (error as Error).message })
    } finally {
      setRunning(false)
    }
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
          <button className="button primary" onClick={simulate} disabled={running}><Play size={16} fill="currentColor" />{running ? 'Running…' : 'Run simulation'}</button>
        </div>
      </header>

      <main className="workspace">
        <aside className="palette panel">
          <div className="panel-heading"><div><span className="eyebrow">Components</span><h2>Service palette</h2></div></div>
          <p className="panel-help">Start from a YAML file or add services and connect them manually.</p>
          <div className="import-box">
            <input ref={fileInput} type="file" accept=".yaml,.yml,application/yaml,text/yaml" hidden onChange={(event) => { const file = event.target.files?.[0]; if (file) void loadYAML(file); event.target.value = '' }} />
            <button className="button import" onClick={() => fileInput.current?.click()}><Upload size={15} /> Load YAML file</button>
          </div>
          <div className="palette-groups">
            {Object.entries(groups).map(([category, services]) => (
              <section key={category}><h3>{category}</h3>{services.map((service) => (
                <button className="palette-item" key={service.type} onClick={() => addService(service)}>
                  <span className={`palette-icon category-${category}`}><Zap size={17} /></span>
                  <span><strong>{service.label}</strong><small>{service.type}</small></span><Plus size={15} />
                </button>
              ))}</section>
            ))}
          </div>
          <button className="button reset" onClick={clearArchitecture}><Trash2 size={15} /> Clear canvas</button>
        </aside>

        <section className="canvas-wrap">
          <div className="canvas-heading"><div><span className="eyebrow">Architecture</span><strong>{nodes.length} nodes · {edges.length} connections</strong></div><span className="canvas-tip">Select a node to configure it</span></div>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            nodeTypes={nodeTypes}
            fitView
            minZoom={0.35}
            maxZoom={1.6}
            defaultEdgeOptions={{ style: { stroke: '#6f8095', strokeWidth: 2 } }}
          >
            <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="#293747" />
            <Controls showInteractive={false} />
            <MiniMap pannable zoomable nodeColor={(node) => node.data.category === 'database' ? '#2d79c7' : node.data.category === 'compute' ? '#9b62d1' : '#e39018'} />
          </ReactFlow>
          {nodes.length === 0 && <div className="canvas-empty"><div className="canvas-empty__icon"><Workflow size={28} /></div><strong>Build your first architecture</strong><p>Add services from the palette and connect them, or load an existing Infra-Sim YAML file.</p><button className="button primary" onClick={() => fileInput.current?.click()}><Upload size={15} /> Choose YAML file</button></div>}
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
          {bottomTab === 'results' && <Results result={result} />}
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

function Results({ result }: { result: RunResponse | null }) {
  if (!result) return <div className="results-empty"><Play size={24} /><div><strong>No simulation results yet</strong><p>Validate the graph, then run the simulation to inspect throughput, latency, and bottlenecks.</p></div></div>
  const data = result.result
  const primary = data.bottleneck.ranked_resources?.find((item) => item.is_bottleneck)
  const resource = result.resources.find((item) => item.resource_id === primary?.resource_id)
  return <div className="results-grid">
    <div className="metric-cards">
      <Metric label="Requests served" value={data.latency.count.toLocaleString()} detail={`${data.trace.TotalEventsProcessed.toLocaleString()} events`} />
      <Metric label="p95 latency" value={formatLatency(data.latency.p95_us)} detail={`p50 ${formatLatency(data.latency.p50_us)}`} />
      <Metric label="p99 latency" value={formatLatency(data.latency.p99_us)} detail={`mean ${formatLatency(data.latency.mean_us)}`} />
      <Metric label="Primary bottleneck" value={resource?.node_id ?? 'None detected'} detail={primary ? `${primary.utilization_pct.toFixed(0)}% utilized` : 'No sustained queue growth'} warning={Boolean(primary)} />
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
