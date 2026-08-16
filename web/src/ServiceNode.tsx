import { Activity, Boxes, Database, Network, Server } from 'lucide-react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import type { ServiceNode } from './types'

const icons = {
  pulse: Activity,
  alb: Network,
  ec2: Server,
  ecs: Boxes,
  rds: Database,
}

export default function ServiceNodeCard({ data, selected }: NodeProps<ServiceNode>) {
  const Icon = icons[data.icon as keyof typeof icons] ?? Server
  return (
    <div className={`service-node category-${data.category} ${selected ? 'selected' : ''}`}>
      <Handle type="target" position={Position.Left} className="node-handle" />
      <div className="service-node__icon"><Icon size={22} strokeWidth={1.8} /></div>
      <div>
        <strong>{data.label}</strong>
        <span>{data.resourceType}</span>
      </div>
      <Handle type="source" position={Position.Right} className="node-handle" />
    </div>
  )
}
