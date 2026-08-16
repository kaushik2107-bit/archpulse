package resources

import (
	"testing"

	"infra-sim/internal/kernel"
)

func TestComputeFailureTargetsOneInstance(t *testing.T) {
	compute := &ComputeResource{ResourceID: 2, Downstream: 3, assigned: map[kernel.RequestID]int{}, Instances: []*ComputeInstance{
		{LatencyMultiplier: 1, Pool: &ServerPool{Capacity: 1, ReportedCapacity: 1, ServiceTime: LognormalSampler{MeanMs: 1}}},
		{LatencyMultiplier: 1, Pool: &ServerPool{Capacity: 1, ReportedCapacity: 1, ServiceTime: LognormalSampler{MeanMs: 1}}},
	}}
	compute.ApplyFailure(kernel.ResourceDegradedPayload{Instance: 2, LatencyMultiplier: 5})
	snapshot := compute.SnapshotMetrics()
	if snapshot.Instances[0].Degraded || !snapshot.Instances[1].Degraded {
		t.Fatalf("unexpected degraded members: %+v", snapshot.Instances)
	}

	request := &kernel.RequestState{ID: 10}
	compute.assigned[request.ID] = 1
	events := compute.HandleEvent(kernel.Event{Time: kernel.Second, Type: kernel.DownstreamCallCompleted, Payload: kernel.DownstreamCallPayload{Request: request}}, &kernel.SimContext{RNG: kernel.NewRNGStreams(1)})
	if len(events) != 1 || events[0].Time != kernel.Second+5*kernel.Millisecond {
		t.Fatalf("targeted degraded service completion = %+v", events)
	}
}

func TestComputeDistributesRequestsAcrossInstances(t *testing.T) {
	compute := &ComputeResource{ResourceID: 2, Downstream: 3, assigned: map[kernel.RequestID]int{}, Instances: []*ComputeInstance{
		{LatencyMultiplier: 1, Pool: &ServerPool{Capacity: 1, ServiceTime: LognormalSampler{MeanMs: 1}}},
		{LatencyMultiplier: 1, Pool: &ServerPool{Capacity: 1, ServiceTime: LognormalSampler{MeanMs: 1}}},
	}}
	ctx := &kernel.SimContext{RNG: kernel.NewRNGStreams(1)}
	for id := kernel.RequestID(1); id <= 2; id++ {
		compute.HandleEvent(kernel.Event{Type: kernel.RequestArrived, Payload: kernel.RequestArrivedPayload{Request: &kernel.RequestState{ID: id}}}, ctx)
	}
	if compute.Instances[0].Pool.InFlight != 1 || compute.Instances[1].Pool.InFlight != 1 {
		t.Fatalf("requests were not distributed: %d/%d", compute.Instances[0].Pool.InFlight, compute.Instances[1].Pool.InFlight)
	}
}
