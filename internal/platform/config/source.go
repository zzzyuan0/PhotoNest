package config

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

type SecretResolver func(ctx context.Context, ref string) (string, error)

type ResolveOptions struct {
	LookupEnv        func(string) (string, bool)
	ResolveSecretRef SecretResolver
}

type SecretValue struct {
	Value      string           `yaml:"value"`
	Env        string           `yaml:"env"`
	SecretRef  string           `yaml:"secretRef"`
	Encrypted  *EncryptedSecret `yaml:"encrypted"`
	AllowEmpty bool             `yaml:"allowEmpty"`
}

type EncryptedSecret struct {
	Ciphertext string `yaml:"ciphertext"`
	Nonce      string `yaml:"nonce"`
	KeyEnv     string `yaml:"keyEnv"`
	AAD        string `yaml:"aad"`
}

func (s SecretValue) Resolve(ctx context.Context, opts ResolveOptions) (string, error) {
	switch {
	case s.Value != "":
		return s.Value, nil
	case s.Env != "":
		lookup := opts.LookupEnv
		if lookup == nil {
			lookup = os.LookupEnv
		}
		if value, ok := lookup(s.Env); ok {
			return value, nil
		}
		if s.AllowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("environment variable %q is not set", s.Env)
	case s.SecretRef != "":
		if opts.ResolveSecretRef == nil {
			return "", errors.New("secret reference resolver is not configured")
		}
		return opts.ResolveSecretRef(ctx, s.SecretRef)
	case s.Encrypted != nil:
		return decryptAESGCM(*s.Encrypted, opts)
	case s.AllowEmpty:
		return "", nil
	default:
		return "", errors.New("no secret source configured")
	}
}

func (s SecretValue) Summary() string {
	switch {
	case s.Value != "":
		return maskedLabel("inline")
	case s.Env != "":
		return fmt.Sprintf("env:%s", s.Env)
	case s.SecretRef != "":
		return fmt.Sprintf("secret-ref:%s", s.SecretRef)
	case s.Encrypted != nil:
		return fmt.Sprintf("encrypted:%s", s.Encrypted.KeyEnv)
	default:
		return "unset"
	}
}

func (s SecretValue) IsConfigured() bool {
	return s.Value != "" || s.Env != "" || s.SecretRef != "" || s.Encrypted != nil || s.AllowEmpty
}

func decryptAESGCM(secret EncryptedSecret, opts ResolveOptions) (string, error) {
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}

	keyRaw, ok := lookup(secret.KeyEnv)
	if !ok {
		return "", fmt.Errorf("missing decryption key env %q", secret.KeyEnv)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(keyRaw)
	if err != nil {
		return "", fmt.Errorf("decode key env %q: %w", secret.KeyEnv, err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(secret.Ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(secret.Nonce)
	if err != nil {
		return "", fmt.Errorf("decode nonce: %w", err)
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", fmt.Errorf("init cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("init gcm: %w", err)
	}

	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(secret.AAD))
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}

	return string(plain), nil
}

func maskedLabel(kind string) string {
	return fmt.Sprintf("%s:********", kind)
}
