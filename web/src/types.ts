import type { Edge, Node } from '@xyflow/react'

export type ParameterValue = number | string | boolean

export interface ConfigurationField {
  key: string
  label: string
  kind: 'integer' | 'number'
  min?: number
  step?: number
  unit?: string
}

export interface CatalogEntry {
  type: string
  icon: string
  label: string
  category: string
  defaults: Record<string, ParameterValue>
  parameters?: ConfigurationField[]
}

export interface ServiceNodeData extends Record<string, unknown> {
  label: string
  resourceType: string
  category: string
  icon: string
  parameters: Record<string, ParameterValue>
  pressure?: 'idle' | 'active' | 'warning' | 'critical'
  liveUtilization?: number
  liveQueueDepth?: number
  liveInstances?: InstancePressure[]
}

export type ServiceNode = Node<ServiceNodeData, 'service'>
export type ArchitectureEdge = Edge

export interface WorkloadSegment {
  type: 'constant' | 'ramp'
  rate?: number
  start_rate?: number
  end_rate?: number
  start_time_s: number
  end_time_s: number
}

export interface FailureConfig {
  target: string
  instance?: number
  at_s: number
  latency_multiplier: number
}

export interface SimulationPayload {
  graph: {
    nodes: Array<{ id: string; name?: string; resource_type: string; parameters: Record<string, ParameterValue> }>
    edges: Array<{ from: string; to: string }>
  }
  workload: { segments: WorkloadSegment[] }
  failures: FailureConfig[]
}

export type ImportedArchitecture = SimulationPayload

export interface MetricPoint {
  time_ns: number
  value: number
}

export interface ResourceVerdict {
  resource_id: number
  utilization_pct: number
  queue_depth: number
  queue_slope_per_sec: number
  peak_utilization_pct: number
  max_queue_depth: number
  final_queue_depth: number
  saturation_fraction_pct: number
  first_queued_time_ns?: number
  score: number
  classification: 'healthy' | 'transient_pressure' | 'constraint' | 'root_constraint' | 'upstream_symptom'
  is_bottleneck: boolean
  reason: string
}

export interface RunResponse {
  result: {
    seed: number
    trace: { TotalEventsProcessed: number; FinalTime: number }
    throughput_rps: MetricPoint[] | null
    latency: { count: number; mean_us: number; p50_us: number; p95_us: number; p99_us: number }
    rejected: number
    resource_timelines: Array<{
      resource_id: number
      utilization_pct: MetricPoint[]
      queue_depth: MetricPoint[]
      instances?: Array<{
        instance: number
        utilization_pct: MetricPoint[]
        queue_depth: MetricPoint[]
        degraded: MetricPoint[]
      }>
    }>
    bottleneck: {
      plateau_start_time_ns: number
      plateau_throughput_rps: number
      throughput_drops?: Array<{ time_ns: number; before_rps: number; after_rps: number; drop_pct: number }>
      ranked_resources: ResourceVerdict[]
    }
  }
  resources: Array<{ resource_id: number; node_id: string; name?: string; type: string }>
}

export interface JobProgress {
  virtual_time_ns: number
  horizon_ns: number
  percent: number
  events_processed: number
  completed_requests: number
  queued_requests: number
  sampling_factor: number
  estimated_arrivals: number
  resources: Array<{
    resource_id: number
    in_flight: number
    queue_depth: number
    capacity: number
    utilization_pct: number
    instances?: InstancePressure[]
  }>
}

export interface InstancePressure {
  instance: number
  in_flight: number
  queue_depth: number
  capacity: number
  utilization_pct: number
  degraded: boolean
}

export interface SimulationJob {
  simulation_id: string
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'
  progress: JobProgress
  resources: Array<{ resource_id: number; node_id: string; name?: string; type: string }>
  result?: RunResponse['result']
  error?: string
}
