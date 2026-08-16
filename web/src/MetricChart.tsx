import type { MetricPoint } from './types'

export default function MetricChart({ points }: { points: MetricPoint[] }) {
  if (points.length < 2) return <div className="chart-empty">Not enough throughput samples</div>
  const width = 720
  const height = 150
  const padding = 12
  const max = Math.max(...points.map((point) => point.value), 1)
  const minTime = points[0].time_ns
  const maxTime = points[points.length - 1].time_ns
  const span = Math.max(maxTime - minTime, 1)
  const path = points.map((point, index) => {
    const x = padding + ((point.time_ns - minTime) / span) * (width - padding * 2)
    const y = height - padding - (point.value / max) * (height - padding * 2)
    return `${index === 0 ? 'M' : 'L'} ${x.toFixed(1)} ${y.toFixed(1)}`
  }).join(' ')
  const area = `${path} L ${width - padding} ${height - padding} L ${padding} ${height - padding} Z`
  return (
    <div className="metric-chart">
      <div className="metric-chart__scale"><span>{Math.round(max).toLocaleString()} RPS</span><span>0</span></div>
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Throughput over simulation time">
        <defs>
          <linearGradient id="throughput-fill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#ff9900" stopOpacity=".36" />
            <stop offset="100%" stopColor="#ff9900" stopOpacity="0" />
          </linearGradient>
        </defs>
        <path d={area} fill="url(#throughput-fill)" />
        <path d={path} fill="none" stroke="#ffad21" strokeWidth="2.5" vectorEffect="non-scaling-stroke" />
      </svg>
    </div>
  )
}
