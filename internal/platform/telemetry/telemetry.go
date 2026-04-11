package telemetry

import "github.com/photonest/photonest/internal/platform/redaction"

type Snapshot struct {
	Metric string         `json:"metric"`
	Labels map[string]string `json:"labels,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

func (s Snapshot) RedactedData() map[string]any {
	return redaction.RedactMap(s.Data)
}
