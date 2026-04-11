package job

import "github.com/photonest/photonest/internal/platform/redaction"

type Payload struct {
	TaskID          string         `json:"taskId"`
	AssetID         string         `json:"assetId,omitempty"`
	ImportSessionID string         `json:"importSessionId,omitempty"`
	Operation       string         `json:"operation"`
	Stage           string         `json:"stage,omitempty"`
	RetryCount      int            `json:"retryCount,omitempty"`
	Debug           map[string]any `json:"debug,omitempty"`
}

func (p Payload) RedactedDebug() map[string]any {
	return redaction.RedactMap(p.Debug)
}
