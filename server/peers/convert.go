package peers

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/jcsvwinston/orbit/server/nodes"

	adminv1 "github.com/jcsvwinston/orbit/proto/gen/go/nucleus/admin/v1"
)

// NodeToProto renders a registry entry for the wire, as ListNodes does.
func NodeToProto(n nodes.NodeInfo) *adminv1.NodeInfo {
	out := &adminv1.NodeInfo{
		NodeId:      n.NodeID,
		Version:     n.Version,
		Labels:      cloneLabels(n.Labels),
		Connected:   n.Connected,
		HostMetrics: n.HostMetrics,
	}
	if !n.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(n.StartedAt)
	}
	if !n.LastSeenAt.IsZero() {
		out.LastSeenAt = timestamppb.New(n.LastSeenAt)
	}
	return out
}

// NodeFromProto reads a peer's announcement into a registry snapshot.
func NodeFromProto(p *adminv1.NodeInfo) nodes.NodeInfo {
	n := nodes.NodeInfo{
		NodeID:      p.GetNodeId(),
		Version:     p.GetVersion(),
		Labels:      cloneLabels(p.GetLabels()),
		Connected:   p.GetConnected(),
		HostMetrics: p.GetHostMetrics(),
	}
	if p.GetStartedAt() != nil {
		n.StartedAt = p.GetStartedAt().AsTime()
	}
	if p.GetLastSeenAt() != nil {
		n.LastSeenAt = p.GetLastSeenAt().AsTime()
	}
	return n
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func timestampOf(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return timestamppb.Now()
	}
	return timestamppb.New(t)
}
