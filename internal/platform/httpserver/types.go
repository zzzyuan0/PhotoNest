package httpserver

type ErrorResponse struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	TraceID string         `json:"traceId"`
	Details map[string]any `json:"details,omitempty"`
}

type CursorPageInfo struct {
	NextCursor  string `json:"nextCursor,omitempty"`
	HasNextPage bool   `json:"hasNextPage"`
}
