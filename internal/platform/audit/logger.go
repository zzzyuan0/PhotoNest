package audit

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/photonest/photonest/internal/platform/telemetry"
)

type Result string

const (
	ResultSuccess       Result = "success"
	ResultDenied        Result = "denied"
	ResultInvalid       Result = "invalid"
	ResultUnimplemented Result = "unimplemented"
	ResultError         Result = "error"
)

type Event struct {
	Timestamp  time.Time      `json:"timestamp"`
	Action     string         `json:"action"`
	Result     Result         `json:"result"`
	SubjectID  string         `json:"subjectId,omitempty"`
	SessionID  string         `json:"sessionId,omitempty"`
	LibraryID  string         `json:"libraryId,omitempty"`
	TargetType string         `json:"targetType,omitempty"`
	TargetID   string         `json:"targetId,omitempty"`
	Method     string         `json:"method"`
	Path       string         `json:"path"`
	RemoteAddr string         `json:"remoteAddr,omitempty"`
	UserAgent  string         `json:"userAgent,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Logger struct {
	now      func() time.Time
	recorder telemetry.Recorder
}

func NewLogger(recorders ...telemetry.Recorder) *Logger {
	var recorder telemetry.Recorder
	if len(recorders) > 0 {
		recorder = recorders[0]
	}
	return &Logger{
		now:      time.Now,
		recorder: recorder,
	}
}

func (l *Logger) Record(_ context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = l.now().UTC()
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("audit marshal error: %v", err)
		return
	}

	if l.recorder != nil && (event.Result == ResultDenied || event.Result == ResultInvalid || event.Result == ResultError) {
		l.recorder.Record(telemetry.Snapshot{
			Metric: "audit.anomaly",
			Labels: map[string]string{
				"action": string(event.Action),
				"result": string(event.Result),
			},
			Data: map[string]any{
				"path":      event.Path,
				"method":    event.Method,
				"libraryId": event.LibraryID,
			},
		})
	}

	log.Printf("audit %s", payload)
}
