package storage

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"
)

type memoryProvider struct {
	name      string
	bucket    string
	keyPrefix string

	mu      sync.RWMutex
	objects map[string]memoryObject
}

type memoryObject struct {
	ref           ObjectRef
	contentType   string
	contentLength int64
	etag          string
	versionID     string
	metadata      map[string]string
	payload       []byte
	lastModified  time.Time
}

func NewMemoryProvider(name string, bucket string, keyPrefix string) Provider {
	return &memoryProvider{
		name:      fallbackString(name, "memory"),
		bucket:    fallbackString(bucket, "memory-bucket"),
		keyPrefix: keyPrefix,
		objects:   map[string]memoryObject{},
	}
}

func (p *memoryProvider) Name() string {
	return p.name
}

func (p *memoryProvider) Capabilities() Capabilities {
	return Capabilities{
		SupportsMultipart: true,
		SupportsMetadata:  true,
		SupportsPresign:   true,
	}
}

func (p *memoryProvider) PutObject(_ context.Context, input PutObjectInput) (ObjectInfo, error) {
	ref := p.qualifyRef(input.Ref)
	payload, err := io.ReadAll(input.Body)
	if err != nil {
		return ObjectInfo{}, err
	}
	sum := md5.Sum(payload)
	now := time.Now().UTC()

	p.mu.Lock()
	p.objects[p.objectKey(ref)] = memoryObject{
		ref:           ref,
		contentType:   strings.TrimSpace(input.ContentType),
		contentLength: int64(len(payload)),
		etag:          hex.EncodeToString(sum[:]),
		versionID:     strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", ""),
		metadata:      cloneStringMap(input.Metadata),
		payload:       payload,
		lastModified:  now,
	}
	p.mu.Unlock()

	return p.HeadObject(context.Background(), ref)
}

func (p *memoryProvider) GetObject(_ context.Context, ref ObjectRef) (io.ReadCloser, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	object, ok := p.objects[p.objectKey(p.qualifyRef(ref))]
	if !ok {
		return nil, fmt.Errorf("object not found")
	}

	return io.NopCloser(bytes.NewReader(object.payload)), nil
}

func (p *memoryProvider) DeleteObject(_ context.Context, ref ObjectRef) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.objects, p.objectKey(p.qualifyRef(ref)))
	return nil
}

func (p *memoryProvider) ListObjects(_ context.Context, input ListObjectsInput) ([]ObjectInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	prefix := p.qualifyKey(input.Prefix)
	items := make([]ObjectInfo, 0, len(p.objects))
	for _, object := range p.objects {
		if prefix != "" && !strings.HasPrefix(object.ref.Key, prefix) {
			continue
		}
		items = append(items, p.toObjectInfo(object))
		if input.Limit > 0 && len(items) >= input.Limit {
			break
		}
	}

	return items, nil
}

func (p *memoryProvider) HeadObject(_ context.Context, ref ObjectRef) (ObjectInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	object, ok := p.objects[p.objectKey(p.qualifyRef(ref))]
	if !ok {
		return ObjectInfo{}, fmt.Errorf("object not found")
	}

	return p.toObjectInfo(object), nil
}

func (p *memoryProvider) ObjectExists(ctx context.Context, ref ObjectRef) (bool, error) {
	_, err := p.HeadObject(ctx, ref)
	if err != nil {
		return false, nil
	}
	return true, nil
}

func (p *memoryProvider) PresignUpload(_ context.Context, input PresignUploadInput) (PresignedRequest, error) {
	ref := p.qualifyRef(input.Ref)
	return PresignedRequest{
		Method:    "PUT",
		URL:       fmt.Sprintf("memory://%s/%s/%s", p.name, ref.Bucket, ref.Key),
		Headers:   cloneStringMap(input.Headers),
		ExpiresAt: time.Now().UTC().Add(input.ExpiresIn),
	}, nil
}

func (p *memoryProvider) PresignDownload(_ context.Context, input PresignDownloadInput) (PresignedRequest, error) {
	ref := p.qualifyRef(input.Ref)
	return PresignedRequest{
		Method:    "GET",
		URL:       fmt.Sprintf("memory://%s/%s/%s", p.name, ref.Bucket, ref.Key),
		ExpiresAt: time.Now().UTC().Add(input.ExpiresIn),
	}, nil
}

func (p *memoryProvider) BeginMultipartUpload(_ context.Context, input BeginMultipartUploadInput) (MultipartUpload, error) {
	ref := p.qualifyRef(input.Ref)
	parts := make([]MultipartPart, 0, input.PartCount)
	for index := 1; index <= input.PartCount; index++ {
		parts = append(parts, MultipartPart{
			PartNumber: index,
			UploadURL:  fmt.Sprintf("memory://%s/%s/%s/parts/%d", p.name, ref.Bucket, ref.Key, index),
		})
	}

	return MultipartUpload{
		UploadID:  strings.ReplaceAll(time.Now().UTC().Format(time.RFC3339Nano), ":", ""),
		Ref:       ref,
		Parts:     parts,
		ExpiresAt: time.Now().UTC().Add(input.ExpiresIn),
	}, nil
}

func (p *memoryProvider) CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (ObjectInfo, error) {
	return p.HeadObject(ctx, input.Ref)
}

func (p *memoryProvider) qualifyRef(ref ObjectRef) ObjectRef {
	return ObjectRef{
		Bucket: fallbackString(ref.Bucket, p.bucket),
		Key:    p.qualifyKey(ref.Key),
	}
}

func (p *memoryProvider) qualifyKey(key string) string {
	trimmedPrefix := strings.Trim(strings.TrimSpace(p.keyPrefix), "/")
	trimmedKey := strings.Trim(strings.TrimSpace(key), "/")
	if trimmedPrefix == "" {
		return trimmedKey
	}
	if trimmedKey == "" {
		return trimmedPrefix
	}
	if trimmedKey == trimmedPrefix || strings.HasPrefix(trimmedKey, trimmedPrefix+"/") {
		return trimmedKey
	}
	return path.Join(trimmedPrefix, trimmedKey)
}

func (p *memoryProvider) objectKey(ref ObjectRef) string {
	return ref.Bucket + ":" + ref.Key
}

func (p *memoryProvider) toObjectInfo(object memoryObject) ObjectInfo {
	return ObjectInfo{
		Ref:           object.ref,
		ETag:          object.etag,
		VersionID:     object.versionID,
		ContentLength: object.contentLength,
		ContentType:   object.contentType,
		Metadata:      cloneStringMap(object.metadata),
		LastModified:  object.lastModified,
	}
}
