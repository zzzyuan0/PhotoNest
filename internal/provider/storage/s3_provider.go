package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	platformconfig "github.com/photonest/photonest/internal/platform/config"
)

type s3Provider struct {
	name                string
	kind                string
	bucket              string
	keyPrefix           string
	publicReadBlockMode string
	client              *s3.Client
	presignClient       *s3.PresignClient
}

func newS3Provider(ctx context.Context, cfg platformconfig.ObjectStorageProviderConfig) (Provider, error) {
	accessKeyID, err := cfg.AccessKeyID.Resolve(ctx, platformconfig.ResolveOptions{})
	if err != nil {
		return nil, fmt.Errorf("resolve accessKeyId: %w", err)
	}
	accessKeySecret, err := cfg.AccessKeySecret.Resolve(ctx, platformconfig.ResolveOptions{})
	if err != nil {
		return nil, fmt.Errorf("resolve accessKeySecret: %w", err)
	}
	sessionToken, err := cfg.SessionToken.Resolve(ctx, platformconfig.ResolveOptions{})
	if err != nil {
		return nil, fmt.Errorf("resolve sessionToken: %w", err)
	}

	awsCfg, err := awscfg.LoadDefaultConfig(
		ctx,
		awscfg.WithRegion(cfg.Region),
		awscfg.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, accessKeySecret, sessionToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("load storage sdk config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.ForcePathStyle
		if endpoint := strings.TrimSpace(cfg.Endpoint); endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &s3Provider{
		name:                cfg.Name,
		kind:                cfg.Kind,
		bucket:              cfg.Bucket,
		keyPrefix:           cfg.KeyPrefix,
		publicReadBlockMode: cfg.PublicReadBlockMode,
		client:              client,
		presignClient:       s3.NewPresignClient(client),
	}, nil
}

func (p *s3Provider) Name() string {
	return p.name
}

func (p *s3Provider) Capabilities() Capabilities {
	return Capabilities{
		SupportsMultipart: true,
		SupportsMetadata:  true,
		SupportsPresign:   true,
	}
}

func (p *s3Provider) PutObject(ctx context.Context, input PutObjectInput) (ObjectInfo, error) {
	ref := p.qualifyRef(input.Ref)
	request := &s3.PutObjectInput{
		Bucket:      aws.String(ref.Bucket),
		Key:         aws.String(ref.Key),
		Body:        input.Body,
		ContentType: aws.String(strings.TrimSpace(input.ContentType)),
		Metadata:    cloneStringMap(input.Metadata),
	}
	if input.ContentLength > 0 {
		request.ContentLength = aws.Int64(input.ContentLength)
	}

	if _, err := p.client.PutObject(ctx, request); err != nil {
		return ObjectInfo{}, classifyStorageError(err)
	}

	return p.HeadObject(ctx, ref)
}

func (p *s3Provider) GetObject(ctx context.Context, ref ObjectRef) (io.ReadCloser, error) {
	qualified := p.qualifyRef(ref)
	output, err := p.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(qualified.Bucket),
		Key:    aws.String(qualified.Key),
	})
	if err != nil {
		return nil, classifyStorageError(err)
	}

	return output.Body, nil
}

func (p *s3Provider) DeleteObject(ctx context.Context, ref ObjectRef) error {
	qualified := p.qualifyRef(ref)
	if _, err := p.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(qualified.Bucket),
		Key:    aws.String(qualified.Key),
	}); err != nil {
		return classifyStorageError(err)
	}

	return nil
}

func (p *s3Provider) ListObjects(ctx context.Context, input ListObjectsInput) ([]ObjectInfo, error) {
	bucket := strings.TrimSpace(input.Bucket)
	if bucket == "" {
		bucket = p.bucket
	}

	prefix := p.qualifyKey(input.Prefix)
	request := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	}
	if input.Limit > 0 {
		request.MaxKeys = aws.Int32(int32(input.Limit))
	}

	output, err := p.client.ListObjectsV2(ctx, request)
	if err != nil {
		return nil, classifyStorageError(err)
	}

	items := make([]ObjectInfo, 0, len(output.Contents))
	for _, item := range output.Contents {
		items = append(items, ObjectInfo{
			Ref: ObjectRef{
				Bucket: bucket,
				Key:    aws.ToString(item.Key),
			},
			ETag:          normalizeETag(aws.ToString(item.ETag)),
			ContentLength: aws.ToInt64(item.Size),
			LastModified:  aws.ToTime(item.LastModified),
		})
	}

	return items, nil
}

func (p *s3Provider) HeadObject(ctx context.Context, ref ObjectRef) (ObjectInfo, error) {
	qualified := p.qualifyRef(ref)
	output, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(qualified.Bucket),
		Key:    aws.String(qualified.Key),
	})
	if err != nil {
		return ObjectInfo{}, classifyStorageError(err)
	}

	return headObjectInfo(qualified, output), nil
}

func (p *s3Provider) ObjectExists(ctx context.Context, ref ObjectRef) (bool, error) {
	_, err := p.HeadObject(ctx, ref)
	if err == nil {
		return true, nil
	}
	if isObjectNotFound(err) {
		return false, nil
	}

	return false, err
}

func (p *s3Provider) PresignUpload(ctx context.Context, input PresignUploadInput) (PresignedRequest, error) {
	policy, err := NewUploadPolicy(input.Ref, input.ExpiresIn, input.ContentType)
	if err != nil {
		return PresignedRequest{}, err
	}

	ref := p.qualifyRef(policy.Ref)
	contentType, headers, metadata := splitUploadHeaders(input.ContentType, input.Headers)
	request := &s3.PutObjectInput{
		Bucket:      aws.String(ref.Bucket),
		Key:         aws.String(ref.Key),
		ContentType: aws.String(contentType),
		Metadata:    metadata,
	}
	if input.ContentLength > 0 {
		request.ContentLength = aws.Int64(input.ContentLength)
	}
	for name, value := range headers {
		if strings.EqualFold(name, "cache-control") {
			request.CacheControl = aws.String(value)
		}
	}

	output, err := p.presignClient.PresignPutObject(ctx, request, func(options *s3.PresignOptions) {
		options.Expires = policy.ExpiresIn
	})
	if err != nil {
		return PresignedRequest{}, classifyStorageError(err)
	}

	return PresignedRequest{
		Method:    output.Method,
		URL:       output.URL,
		Headers:   headerMap(output.SignedHeader),
		ExpiresAt: time.Now().UTC().Add(policy.ExpiresIn),
	}, nil
}

func (p *s3Provider) PresignDownload(ctx context.Context, input PresignDownloadInput) (PresignedRequest, error) {
	policy, err := NewDownloadPolicy(input.Ref, input.ExpiresIn)
	if err != nil {
		return PresignedRequest{}, err
	}

	ref := p.qualifyRef(policy.Ref)
	output, err := p.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(ref.Bucket),
		Key:    aws.String(ref.Key),
	}, func(options *s3.PresignOptions) {
		options.Expires = policy.ExpiresIn
	})
	if err != nil {
		return PresignedRequest{}, classifyStorageError(err)
	}

	return PresignedRequest{
		Method:    output.Method,
		URL:       output.URL,
		Headers:   headerMap(output.SignedHeader),
		ExpiresAt: time.Now().UTC().Add(policy.ExpiresIn),
	}, nil
}

func (p *s3Provider) BeginMultipartUpload(ctx context.Context, input BeginMultipartUploadInput) (MultipartUpload, error) {
	if input.PartCount <= 0 {
		return MultipartUpload{}, fmt.Errorf("part count must be positive")
	}

	ref := p.qualifyRef(input.Ref)
	output, err := p.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(ref.Bucket),
		Key:         aws.String(ref.Key),
		ContentType: aws.String(input.ContentType),
		Metadata:    cloneStringMap(input.Metadata),
	})
	if err != nil {
		return MultipartUpload{}, classifyStorageError(err)
	}

	parts := make([]MultipartPart, 0, input.PartCount)
	for index := 1; index <= input.PartCount; index++ {
		part, presignErr := p.presignClient.PresignUploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(ref.Bucket),
			Key:        aws.String(ref.Key),
			UploadId:   output.UploadId,
			PartNumber: aws.Int32(int32(index)),
		}, func(options *s3.PresignOptions) {
			options.Expires = input.ExpiresIn
		})
		if presignErr != nil {
			return MultipartUpload{}, classifyStorageError(presignErr)
		}
		parts = append(parts, MultipartPart{
			PartNumber: index,
			UploadURL:  part.URL,
			Headers:    headerMap(part.SignedHeader),
		})
	}

	return MultipartUpload{
		UploadID:  aws.ToString(output.UploadId),
		Ref:       ref,
		Parts:     parts,
		ExpiresAt: time.Now().UTC().Add(input.ExpiresIn),
	}, nil
}

func (p *s3Provider) CompleteMultipartUpload(ctx context.Context, input CompleteMultipartUploadInput) (ObjectInfo, error) {
	ref := p.qualifyRef(input.Ref)
	parts := make([]s3types.CompletedPart, 0, len(input.Parts))
	for _, part := range input.Parts {
		parts = append(parts, s3types.CompletedPart{
			ETag:       aws.String(part.ETag),
			PartNumber: aws.Int32(int32(part.PartNumber)),
		})
	}

	if _, err := p.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(ref.Bucket),
		Key:      aws.String(ref.Key),
		UploadId: aws.String(input.UploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{
			Parts: parts,
		},
	}); err != nil {
		return ObjectInfo{}, classifyStorageError(err)
	}

	return p.HeadObject(ctx, ref)
}

func (p *s3Provider) HealthCheck(ctx context.Context) error {
	if _, err := p.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(p.bucket),
	}); err != nil {
		return fmt.Errorf("validate bucket access: %w", classifyStorageError(err))
	}

	if err := p.checkWriteAccess(ctx); err != nil {
		return err
	}

	if err := p.checkPrivateRead(ctx); err != nil {
		return err
	}

	return nil
}

func (p *s3Provider) checkWriteAccess(ctx context.Context) error {
	ref := ObjectRef{
		Bucket: p.bucket,
		Key: path.Join(
			p.keyPrefix,
			".healthchecks",
			strconv.FormatInt(time.Now().UTC().UnixNano(), 10),
		),
	}
	if _, err := p.PutObject(ctx, PutObjectInput{
		Ref:           ref,
		Body:          bytes.NewReader(nil),
		ContentType:   "application/octet-stream",
		ContentLength: 0,
		Metadata: map[string]string{
			"photonest-healthcheck": "true",
		},
	}); err != nil {
		return fmt.Errorf("validate bucket write access: %w", err)
	}

	if err := p.DeleteObject(ctx, ref); err != nil {
		return fmt.Errorf("validate bucket cleanup: %w", err)
	}

	return nil
}

func (p *s3Provider) checkPrivateRead(ctx context.Context) error {
	if strings.TrimSpace(p.publicReadBlockMode) != "fail-fast" {
		return nil
	}

	output, err := p.client.GetBucketAcl(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(p.bucket),
	})
	if err != nil {
		if isUnsupportedOperation(err) {
			return nil
		}
		return fmt.Errorf("inspect bucket acl: %w", classifyStorageError(err))
	}

	for _, grant := range output.Grants {
		if grant.Grantee == nil || grant.Grantee.URI == nil {
			continue
		}

		switch aws.ToString(grant.Grantee.URI) {
		case "http://acs.amazonaws.com/groups/global/AllUsers",
			"http://acs.amazonaws.com/groups/global/AuthenticatedUsers":
			return errors.New("bucket ACL exposes public or authenticated read access")
		}
	}

	return nil
}

func (p *s3Provider) qualifyRef(ref ObjectRef) ObjectRef {
	return ObjectRef{
		Bucket: fallbackString(ref.Bucket, p.bucket),
		Key:    p.qualifyKey(ref.Key),
	}
}

func (p *s3Provider) qualifyKey(key string) string {
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

func headObjectInfo(ref ObjectRef, output *s3.HeadObjectOutput) ObjectInfo {
	return ObjectInfo{
		Ref:           ref,
		ETag:          normalizeETag(aws.ToString(output.ETag)),
		VersionID:     aws.ToString(output.VersionId),
		ContentLength: aws.ToInt64(output.ContentLength),
		ContentType:   aws.ToString(output.ContentType),
		Metadata:      cloneStringMap(output.Metadata),
		LastModified:  aws.ToTime(output.LastModified),
	}
}

func splitUploadHeaders(contentType string, headers map[string]string) (string, map[string]string, map[string]string) {
	metadata := map[string]string{}
	remaining := map[string]string{}
	resolvedContentType := strings.TrimSpace(contentType)

	for name, value := range headers {
		switch {
		case strings.EqualFold(name, "content-type") && resolvedContentType == "":
			resolvedContentType = value
		case strings.HasPrefix(strings.ToLower(name), "x-amz-meta-"):
			metadata[strings.TrimPrefix(strings.ToLower(name), "x-amz-meta-")] = value
		case strings.HasPrefix(strings.ToLower(name), "x-cos-meta-"):
			metadata[strings.TrimPrefix(strings.ToLower(name), "x-cos-meta-")] = value
		default:
			remaining[name] = value
		}
	}

	return resolvedContentType, remaining, metadata
}

func headerMap(header http.Header) map[string]string {
	if len(header) == 0 {
		return nil
	}

	values := make(map[string]string, len(header))
	for name, items := range header {
		values[name] = strings.Join(items, ",")
	}

	return values
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

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), "\"")
}

func fallbackString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}

	return fallback
}

func isObjectNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "NoSuchBucket":
			return true
		}
	}

	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.HTTPStatusCode() == http.StatusNotFound
	}

	return false
}

func isUnsupportedOperation(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotImplemented", "XNotImplemented":
			return true
		}
	}

	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		return responseErr.HTTPStatusCode() == http.StatusNotImplemented
	}

	return false
}

func classifyStorageError(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Errorf("%s: %s: %w", apiErr.ErrorCode(), apiErr.ErrorMessage(), err)
	}

	return err
}
