package storage

import (
	"fmt"
	"time"
)

const (
	MaxUploadTTL   = 15 * time.Minute
	MaxDownloadTTL = 5 * time.Minute
)

type SignedAccessPolicy struct {
	Ref        ObjectRef
	Method     string
	Scope      string
	Permission string
	ExpiresIn  time.Duration
	Headers    map[string]string
}

func NewUploadPolicy(ref ObjectRef, ttl time.Duration, contentType string) (SignedAccessPolicy, error) {
	policy := SignedAccessPolicy{
		Ref:        ref,
		Method:     "PUT",
		Scope:      "single-object",
		Permission: "write",
		ExpiresIn:  ttl,
		Headers: map[string]string{
			"Content-Type": contentType,
		},
	}

	return policy, policy.Validate()
}

func NewDownloadPolicy(ref ObjectRef, ttl time.Duration) (SignedAccessPolicy, error) {
	policy := SignedAccessPolicy{
		Ref:        ref,
		Method:     "GET",
		Scope:      "single-object",
		Permission: "read",
		ExpiresIn:  ttl,
	}

	return policy, policy.Validate()
}

func (p SignedAccessPolicy) Validate() error {
	switch {
	case p.Ref.Bucket == "":
		return fmt.Errorf("bucket is required")
	case p.Ref.Key == "":
		return fmt.Errorf("object key is required")
	case p.Scope != "single-object":
		return fmt.Errorf("scope must remain single-object")
	case p.Permission == "write" && p.ExpiresIn > MaxUploadTTL:
		return fmt.Errorf("upload policy ttl must be <= %s", MaxUploadTTL)
	case p.Permission == "read" && p.ExpiresIn > MaxDownloadTTL:
		return fmt.Errorf("download policy ttl must be <= %s", MaxDownloadTTL)
	case p.ExpiresIn <= 0:
		return fmt.Errorf("ttl must be positive")
	default:
		return nil
	}
}
