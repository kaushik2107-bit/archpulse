import { Activity, Boxes, Database, Network, Server } from 'lucide-react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import type { ServiceNode } from './types'

const icons = { pulse: Activity, alb: Network, ec2: Server, ecs: Boxes, rds: Database }

export default function ServiceNodeCard({ data, selected }: NodeProps<ServiceNode>) {
  const Icon = icons[data.icon as keyof typeof icons] ?? Server
  const instances = data.liveInstances ?? Array.from({ length: Number(data.parameters.instances ?? 0) }, (_, index) => ({ instance: index + 1, utilization_pct: 0, queue_depth: 0, degraded: false, in_flight: 0, capacity: 0 }))
  const clustered = instances.length > 1
  return <div className={`service-node category-${data.category} pressure-${data.pressure ?? 'idle'} ${clustered ? 'service-cluster' : ''} ${selected ? 'selected' : ''}`}>
    <Handle type="target" position={Position.Left} className="node-handle" />
    <div className="service-node__header">
      <div className="service-node__icon"><Icon size={22} strokeWidth={1.8} /></div>
      <div>
        <strong>{data.label}</strong>
        <span>{clustered ? `${instances.length} instance cluster` : data.resourceType}</span>
        {data.pressure && data.pressure !== 'idle' && <em>{Math.round(data.liveUtilization ?? 0)}% · queue {(data.liveQueueDepth ?? 0).toLocaleString()}</em>}
      </div>
    </div>
    {clustered && <div className="instance-grid" aria-label={`${instances.length} instances in ${data.label}`}>
      {instances.map((instance) => {
        const state = instance.degraded || instance.queue_depth > 0 || instance.utilization_pct >= 85 ? 'critical' : instance.utilization_pct >= 50 ? 'warning' : 'healthy'
        return <div key={instance.instance} className={`instance-card ${state}`} title={`Instance ${instance.instance}: ${Math.round(instance.utilization_pct)}% utilized${instance.degraded ? ' · degraded' : ''}`}>
          <Server size={13} strokeWidth={2} /><div><b>Instance {instance.instance}</b><small>{instance.degraded ? 'Degraded' : `${Math.round(instance.utilization_pct)}% · q${instance.queue_depth.toLocaleString()}`}</small></div>
        </div>
      })}
    </div>}
    <Handle type="source" position={Position.Right} className="node-handle" />
  </div>
}
