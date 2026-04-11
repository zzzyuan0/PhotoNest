package audit

import (
	"context"
	"encoding/json"
	"log"
	"time"
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
	now func() time.Time
}

func NewLogger() *Logger {
	return &Logger{
		now: time.Now,
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

	log.Printf("audit %s", payload)
}
