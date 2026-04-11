package storage

import (
	"context"
	"io"
	"time"
)

type Provider interface {
	Name() string
	Capabilities() Capabilities
	PutObject(ctx context.Context, input PutObjectInput) (ObjectInfo, error)
	GetObject(ctx context.Context, ref ObjectRef) (io.ReadCloser, error)
	DeleteObject(ctx context.Context, ref ObjectRef) error
	ListObjects(ctx context.Context, input ListObjectsInput) ([]ObjectInfo, error)
	HeadObject(ctx context.Context, ref ObjectRef) (ObjectInfo, error)
	ObjectExists(ctx context.Context, ref ObjectRef) (bool, error)
	PresignUpload(ctx context.Context, input PresignUploadInput) (PresignedRequest, error)
	PresignDownload(ctx context.Context, input PresignDownloadInput) (PresignedRequest, error)
	BeginMultipartUpload(ctx context.Context, input BeginMultipartUploadInput) (MultipartUpload, error)
	CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (ObjectInfo, error)
}

type Capabilities struct {
	SupportsMultipart bool
	SupportsMetadata  bool
	SupportsPresign   bool
}

type ObjectRef struct {
	Bucket string
	Key    string
}

type ObjectInfo struct {
	Ref           ObjectRef
	ETag          string
	VersionID     string
	ContentLength int64
	ContentType   string
	Metadata      map[string]string
	LastModified  time.Time
}

type PutObjectInput struct {
	Ref          ObjectRef
	Body         io.Reader
	ContentType  string
	ContentLength int64
	Metadata     map[string]string
}

type ListObjectsInput struct {
	Bucket string
	Prefix string
	Limit  int
}

type PresignUploadInput struct {
	Ref           ObjectRef
	ContentType   string
	ContentLength int64
	ExpiresIn     time.Duration
	Headers       map[string]string
}

type PresignDownloadInput struct {
	Ref       ObjectRef
	ExpiresIn time.Duration
}

type PresignedRequest struct {
	Method    string
	URL       string
	Headers   map[string]string
	FormFields map[string]string
	ExpiresAt time.Time
}

type BeginMultipartUploadInput struct {
	Ref         ObjectRef
	ContentType string
	Metadata    map[string]string
	PartCount   int
	ExpiresIn   time.Duration
}

type MultipartUpload struct {
	UploadID  string
	Ref       ObjectRef
	Parts     []MultipartPart
	ExpiresAt time.Time
}

type MultipartPart struct {
	PartNumber int
	UploadURL  string
	Headers    map[string]string
}

type CompletedPart struct {
	PartNumber int
	ETag       string
}

type CompleteMultipartUploadInput struct {
	Ref      ObjectRef
	UploadID string
	Parts    []CompletedPart
}
