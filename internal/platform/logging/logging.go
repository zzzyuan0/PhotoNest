package logging

import "github.com/photonest/photonest/internal/platform/redaction"

type Event struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

func (e Event) RedactedFields() map[string]any {
	return redaction.RedactMap(e.Fields)
}

func (e Event) ToMap() map[string]any {
	return map[string]any{
		"level":   e.Level,
		"message": e.Message,
		"fields":  e.RedactedFields(),
	}
}
