package storage

import (
	"testing"
	"time"
)

func TestUploadPolicyRejectsLongTTL(t *testing.T) {
	_, err := NewUploadPolicy(ObjectRef{
		Bucket: "photos",
		Key:    "library/a.png",
	}, MaxUploadTTL+time.Second, "image/png")
	if err == nil {
		t.Fatal("expected upload policy validation to reject long ttl")
	}
}

func TestDownloadPolicyStaysSingleObjectScoped(t *testing.T) {
	policy, err := NewDownloadPolicy(ObjectRef{
		Bucket: "photos",
		Key:    "library/a.png",
	}, MaxDownloadTTL)
	if err != nil {
		t.Fatalf("new download policy: %v", err)
	}

	policy.Scope = "library"
	if err := policy.Validate(); err == nil {
		t.Fatal("expected scope validation error")
	}
}
