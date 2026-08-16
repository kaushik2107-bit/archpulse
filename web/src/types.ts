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
  at_s: number
  latency_multiplier: number
}

export interface SimulationPayload {
  graph: {
    nodes: Array<{ id: string; resource_type: string; parameters: Record<string, ParameterValue> }>
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
    bottleneck: {
      plateau_start_time_ns: number
      plateau_throughput_rps: number
      ranked_resources: ResourceVerdict[]
    }
  }
  resources: Array<{ resource_id: number; node_id: string; type: string }>
}
