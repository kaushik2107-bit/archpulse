package resources

import "infra-sim/internal/kernel"

type AdmitResult uint8

const (
	Admitted AdmitResult = iota
	Queued
	Rejected
)

type ServerPool struct {
	Capacity         int
	InFlight         int
	Queue            []*kernel.RequestState
	QueueLimit       int
	ServiceTime      ServiceTimeSampler
	MetricScale      int
	ReportedCapacity int
}

func (p *ServerPool) TryAdmit(request *kernel.RequestState) AdmitResult {
	if p.InFlight < p.Capacity {
		p.InFlight++
		return Admitted
	}
	if p.QueueLimit > 0 && len(p.Queue) >= p.QueueLimit {
		return Rejected
	}
	p.Queue = append(p.Queue, request)
	return Queued
}

func (p *ServerPool) OnServiceComplete() *kernel.RequestState {
	if p.InFlight > 0 {
		p.InFlight--
	}
	if len(p.Queue) == 0 {
		return nil
	}
	next := p.Queue[0]
	p.Queue[0] = nil
	p.Queue = p.Queue[1:]
	p.InFlight++
	return next
}
