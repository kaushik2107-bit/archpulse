package resources

import "archpulse/internal/kernel"

type ConnectionPool struct {
	MaxConnections   int
	InUse            int
	Waiters          []*kernel.RequestState
	MetricScale      int
	ReportedCapacity int
}

func (p *ConnectionPool) Acquire(request *kernel.RequestState) AdmitResult {
	if p.InUse < p.MaxConnections {
		p.InUse++
		return Admitted
	}
	p.Waiters = append(p.Waiters, request)
	return Queued
}

func (p *ConnectionPool) Release() *kernel.RequestState {
	if p.InUse > 0 {
		p.InUse--
	}
	if len(p.Waiters) == 0 {
		return nil
	}
	next := p.Waiters[0]
	p.Waiters[0] = nil
	p.Waiters = p.Waiters[1:]
	p.InUse++
	return next
}

func (p *ConnectionPool) UtilizationPct() float64 {
	if p.MaxConnections <= 0 {
		return 0
	}
	return 100 * float64(p.InUse) / float64(p.MaxConnections)
}
