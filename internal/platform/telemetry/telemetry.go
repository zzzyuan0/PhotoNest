package telemetry

import (
	"slices"
	"sync"

	"github.com/photonest/photonest/internal/platform/redaction"
)

type Snapshot struct {
	Metric string         `json:"metric"`
	Labels map[string]string `json:"labels,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

func (s Snapshot) RedactedData() map[string]any {
	return redaction.RedactMap(s.Data)
}

type Recorder interface {
	Record(snapshot Snapshot)
}

type Collector struct {
	mu        sync.RWMutex
	snapshots []Snapshot
}

func NewCollector() *Collector {
	return &Collector{
		snapshots: make([]Snapshot, 0, 64),
	}
}

func (c *Collector) Record(snapshot Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.snapshots = append(c.snapshots, Snapshot{
		Metric: snapshot.Metric,
		Labels: cloneStringMap(snapshot.Labels),
		Data:   cloneAnyMap(snapshot.Data),
	})
	if len(c.snapshots) > 256 {
		c.snapshots = slices.Clone(c.snapshots[len(c.snapshots)-256:])
	}
}

func (c *Collector) Snapshots() []Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	items := make([]Snapshot, 0, len(c.snapshots))
	for _, snapshot := range c.snapshots {
		items = append(items, Snapshot{
			Metric: snapshot.Metric,
			Labels: cloneStringMap(snapshot.Labels),
			Data:   cloneAnyMap(snapshot.Data),
		})
	}
	return items
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
