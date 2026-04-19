package config

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type AppConfig struct {
	Service          ServiceConfig          `yaml:"service"`
	Server           ServerConfig           `yaml:"server"`
	Database         DatabaseConfig         `yaml:"database"`
	Queue            QueueConfig            `yaml:"queue"`
	StorageProviders StorageProvidersConfig `yaml:"storageProviders"`
	AIProviders      []AIProviderConfig     `yaml:"aiProviders"`
	Telemetry        TelemetryConfig        `yaml:"telemetry"`
	Security         SecurityConfig         `yaml:"security"`
}

type ServiceConfig struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment"`
}

type ServerConfig struct {
	Port int `yaml:"port"`
}

type DatabaseConfig struct {
	Host         string      `yaml:"host"`
	Port         int         `yaml:"port"`
	Name         string      `yaml:"name"`
	User         string      `yaml:"user"`
	Password     SecretValue `yaml:"password"`
	DSN          SecretValue `yaml:"dsn"`
	SSLMode      string      `yaml:"sslMode"`
	MaxOpenConns int         `yaml:"maxOpenConns"`
	MaxIdleConns int         `yaml:"maxIdleConns"`
}

type QueueConfig struct {
	Address      string      `yaml:"address"`
	Password     SecretValue `yaml:"password"`
	DB           int         `yaml:"db"`
	Namespace    string      `yaml:"namespace"`
	MaxConsumers int         `yaml:"maxConsumers"`
}

type StorageProvidersConfig struct {
	Primary ObjectStorageProviderConfig   `yaml:"primary"`
	Backup  []ObjectStorageProviderConfig `yaml:"backup"`
}

type ObjectStorageProviderConfig struct {
	Name                string        `yaml:"name"`
	Kind                string        `yaml:"kind"`
	Bucket              string        `yaml:"bucket"`
	Region              string        `yaml:"region"`
	Endpoint            string        `yaml:"endpoint"`
	ForcePathStyle      bool          `yaml:"forcePathStyle"`
	KeyPrefix           string        `yaml:"keyPrefix"`
	AccessKeyID         SecretValue   `yaml:"accessKeyId"`
	AccessKeySecret     SecretValue   `yaml:"accessKeySecret"`
	SessionToken        SecretValue   `yaml:"sessionToken"`
	UploadPresignTTL    time.Duration `yaml:"uploadPresignTTL"`
	DownloadPresignTTL  time.Duration `yaml:"downloadPresignTTL"`
	AllowedOrigins      []string      `yaml:"allowedOrigins"`
	PrivateRead         bool          `yaml:"privateRead"`
	CORSConfigPath      string        `yaml:"corsConfigPath"`
	HealthCheckURL      string        `yaml:"healthCheckURL"`
	PublicReadBlockMode string        `yaml:"publicReadBlockMode"`
}

type AIProviderConfig struct {
	Name              string            `yaml:"name"`
	Kind              string            `yaml:"kind"`
	Endpoint          string            `yaml:"endpoint"`
	Model             string            `yaml:"model"`
	ModelProfile      string            `yaml:"modelProfile"`
	Models            map[string]string `yaml:"models"`
	Capabilities      []string          `yaml:"capabilities"`
	Token             SecretValue       `yaml:"token"`
	Timeout           time.Duration     `yaml:"timeout"`
	AllowRemote       bool              `yaml:"allowRemote"`
	ExecutionBoundary string            `yaml:"executionBoundary"`
	HealthCheckURL    string            `yaml:"healthCheckURL"`
}

type TelemetryConfig struct {
	LogLevel         string `yaml:"logLevel"`
	EnableMetrics    bool   `yaml:"enableMetrics"`
	EnableTracing    bool   `yaml:"enableTracing"`
	EnableStructured bool   `yaml:"enableStructured"`
	RedactionProfile string `yaml:"redactionProfile"`
}

type SecurityConfig struct {
	CSRFEnabled              bool                `yaml:"csrfEnabled"`
	RecentAuthWindow         time.Duration       `yaml:"recentAuthWindow"`
	UploadCredentialTTL      time.Duration       `yaml:"uploadCredentialTTL"`
	DownloadCredentialTTL    time.Duration       `yaml:"downloadCredentialTTL"`
	DebugRetention           time.Duration       `yaml:"debugRetention"`
	StrictPrivateObjectCheck bool                `yaml:"strictPrivateObjectCheck"`
	Session                  SessionConfig       `yaml:"session"`
	BootstrapAuth            BootstrapAuthConfig `yaml:"bootstrapAuth"`
}

type SessionConfig struct {
	CookieName     string        `yaml:"cookieName"`
	CSRFCookieName string        `yaml:"csrfCookieName"`
	CSRFHeaderName string        `yaml:"csrfHeaderName"`
	SigningKey     SecretValue   `yaml:"signingKey"`
	MaxAge         time.Duration `yaml:"maxAge"`
	SecureCookies  bool          `yaml:"secureCookies"`
	SameSite       string        `yaml:"sameSite"`
}

type BootstrapAuthConfig struct {
	Username    string      `yaml:"username"`
	Password    SecretValue `yaml:"password"`
	Subject     string      `yaml:"subject"`
	DisplayName string      `yaml:"displayName"`
	Roles       []string    `yaml:"roles"`
	LibraryIDs  []string    `yaml:"libraryIds"`
}

func DefaultPath() string {
	if value := strings.TrimSpace(os.Getenv("PHOTONEST_CONFIG")); value != "" {
		return value
	}

	return "./config/examples/app.yaml"
}

func LoadFile(path string) (AppConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, fmt.Errorf("read config: %w", err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.Validate(context.Background()); err != nil {
		return AppConfig{}, err
	}

	return cfg, nil
}

func (c *AppConfig) applyDefaults() {
	if c.Service.Name == "" {
		c.Service.Name = "photonest"
	}
	if c.Service.Environment == "" {
		c.Service.Environment = "development"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Database.Port == 0 {
		c.Database.Port = 5432
	}
	if c.Database.SSLMode == "" {
		c.Database.SSLMode = "disable"
	}
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 20
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}
	if c.Queue.Namespace == "" {
		c.Queue.Namespace = "photonest"
	}
	if c.Queue.MaxConsumers == 0 {
		c.Queue.MaxConsumers = 8
	}
	c.StorageProviders.Primary.applyDefaults()
	for i := range c.StorageProviders.Backup {
		c.StorageProviders.Backup[i].applyDefaults()
	}
	for i := range c.AIProviders {
		c.AIProviders[i].applyDefaults()
	}
	if c.Telemetry.LogLevel == "" {
		c.Telemetry.LogLevel = "info"
	}
	if c.Telemetry.RedactionProfile == "" {
		c.Telemetry.RedactionProfile = "strict"
	}
	if c.Security.RecentAuthWindow == 0 {
		c.Security.RecentAuthWindow = 15 * time.Minute
	}
	if c.Security.UploadCredentialTTL == 0 {
		c.Security.UploadCredentialTTL = 15 * time.Minute
	}
	if c.Security.DownloadCredentialTTL == 0 {
		c.Security.DownloadCredentialTTL = 5 * time.Minute
	}
	if c.Security.DebugRetention == 0 {
		c.Security.DebugRetention = 24 * time.Hour
	}
	if c.Security.Session.CookieName == "" {
		c.Security.Session.CookieName = "photonest_session"
	}
	if c.Security.Session.CSRFCookieName == "" {
		c.Security.Session.CSRFCookieName = "photonest_csrf"
	}
	if c.Security.Session.CSRFHeaderName == "" {
		c.Security.Session.CSRFHeaderName = "X-CSRF-Token"
	}
	if c.Security.Session.MaxAge == 0 {
		c.Security.Session.MaxAge = 12 * time.Hour
	}
	if c.Security.Session.SameSite == "" {
		c.Security.Session.SameSite = "strict"
	}
	if c.Security.BootstrapAuth.Username == "" {
		c.Security.BootstrapAuth.Username = "admin"
	}
	if c.Security.BootstrapAuth.Subject == "" {
		c.Security.BootstrapAuth.Subject = "bootstrap-admin"
	}
	if c.Security.BootstrapAuth.DisplayName == "" {
		c.Security.BootstrapAuth.DisplayName = "Bootstrap Admin"
	}
	if len(c.Security.BootstrapAuth.Roles) == 0 {
		c.Security.BootstrapAuth.Roles = []string{"admin"}
	}
}

func (c AppConfig) Validate(ctx context.Context) error {
	if c.Database.Host == "" && !c.Database.DSN.IsConfigured() {
		return fmt.Errorf("database host or dsn must be configured")
	}
	if c.Queue.Address == "" {
		return fmt.Errorf("queue address is required")
	}
	if c.StorageProviders.Primary.Name == "" {
		return fmt.Errorf("primary storage provider name is required")
	}
	if c.StorageProviders.Primary.Bucket == "" {
		return fmt.Errorf("primary storage provider bucket is required")
	}
	if c.StorageProviders.Primary.Kind == "" {
		return fmt.Errorf("primary storage provider kind is required")
	}
	if !c.StorageProviders.Primary.PrivateRead && c.Security.StrictPrivateObjectCheck {
		return fmt.Errorf("primary storage provider must keep private read enabled")
	}
	if _, err := c.Database.EffectiveDSN(ctx); err != nil {
		return fmt.Errorf("database dsn: %w", err)
	}
	if _, err := c.Queue.RedactedSummary(ctx); err != nil {
		return fmt.Errorf("queue config: %w", err)
	}

	for _, provider := range append([]ObjectStorageProviderConfig{c.StorageProviders.Primary}, c.StorageProviders.Backup...) {
		if err := provider.ValidateLeastPrivilege(); err != nil {
			return fmt.Errorf("storage provider %s least-privilege: %w", provider.Name, err)
		}
		if _, err := provider.RedactedSummary(ctx); err != nil {
			return fmt.Errorf("storage provider %s: %w", provider.Name, err)
		}
	}

	for _, provider := range c.AIProviders {
		if err := provider.ValidateLeastPrivilege(); err != nil {
			return fmt.Errorf("ai provider %s least-privilege: %w", provider.Name, err)
		}
		if _, err := provider.RedactedSummary(ctx); err != nil {
			return fmt.Errorf("ai provider %s: %w", provider.Name, err)
		}
	}
	if err := c.Security.Validate(ctx); err != nil {
		return err
	}

	return nil
}

func (c SecurityConfig) Validate(ctx context.Context) error {
	switch {
	case c.RecentAuthWindow <= 0 || c.RecentAuthWindow > 24*time.Hour:
		return fmt.Errorf("recentAuthWindow must be between 1ns and 24h")
	case c.UploadCredentialTTL <= 0 || c.UploadCredentialTTL > 15*time.Minute:
		return fmt.Errorf("uploadCredentialTTL must be between 1ns and 15m")
	case c.DownloadCredentialTTL <= 0 || c.DownloadCredentialTTL > 5*time.Minute:
		return fmt.Errorf("downloadCredentialTTL must be between 1ns and 5m")
	case c.DebugRetention <= 0 || c.DebugRetention > 30*24*time.Hour:
		return fmt.Errorf("debugRetention must be between 1ns and 720h")
	}

	if err := c.Session.Validate(ctx); err != nil {
		return fmt.Errorf("security session: %w", err)
	}
	if err := c.BootstrapAuth.Validate(ctx); err != nil {
		return fmt.Errorf("security bootstrap auth: %w", err)
	}

	return nil
}

func (c SessionConfig) Validate(ctx context.Context) error {
	switch {
	case strings.TrimSpace(c.CookieName) == "":
		return fmt.Errorf("cookieName is required")
	case strings.TrimSpace(c.CSRFCookieName) == "":
		return fmt.Errorf("csrfCookieName is required")
	case strings.TrimSpace(c.CSRFHeaderName) == "":
		return fmt.Errorf("csrfHeaderName is required")
	case c.MaxAge <= 0 || c.MaxAge > 7*24*time.Hour:
		return fmt.Errorf("maxAge must be between 1ns and 168h")
	}

	switch strings.ToLower(strings.TrimSpace(c.SameSite)) {
	case "strict", "lax", "none":
	default:
		return fmt.Errorf("sameSite must be one of strict, lax or none")
	}

	signingKey, err := c.SigningKey.Resolve(ctx, ResolveOptions{})
	if err != nil {
		return fmt.Errorf("resolve signingKey: %w", err)
	}
	if len(signingKey) < 32 {
		return fmt.Errorf("signingKey must be at least 32 bytes")
	}

	return nil
}

func (c BootstrapAuthConfig) Validate(ctx context.Context) error {
	switch {
	case strings.TrimSpace(c.Username) == "":
		return fmt.Errorf("username is required")
	case strings.TrimSpace(c.Subject) == "":
		return fmt.Errorf("subject is required")
	case len(c.Roles) == 0:
		return fmt.Errorf("at least one role is required")
	}

	if _, err := c.Password.Resolve(ctx, ResolveOptions{}); err != nil {
		return fmt.Errorf("resolve password: %w", err)
	}

	return nil
}

func (c DatabaseConfig) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c DatabaseConfig) EffectiveDSN(ctx context.Context) (string, error) {
	if c.DSN.IsConfigured() {
		return c.DSN.Resolve(ctx, ResolveOptions{})
	}

	password, err := c.Password.Resolve(ctx, ResolveOptions{})
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=%s",
		c.User,
		password,
		c.Address(),
		c.Name,
		c.SSLMode,
	), nil
}

func (c DatabaseConfig) RedactedSummary(ctx context.Context) (map[string]any, error) {
	dsn, err := c.EffectiveDSN(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"address":        c.Address(),
		"name":           c.Name,
		"user":           c.User,
		"sslMode":        c.SSLMode,
		"dsn":            maskConnectionString(dsn),
		"maxOpenConns":   c.MaxOpenConns,
		"maxIdleConns":   c.MaxIdleConns,
		"passwordSource": c.Password.Summary(),
	}, nil
}

func (c ObjectStorageProviderConfig) ValidateLeastPrivilege() error {
	switch {
	case strings.TrimSpace(c.Bucket) == "":
		return fmt.Errorf("bucket is required")
	case strings.TrimSpace(c.Region) == "":
		return fmt.Errorf("region is required")
	case strings.TrimSpace(c.Endpoint) == "":
		return fmt.Errorf("endpoint is required")
	case strings.TrimSpace(c.KeyPrefix) == "":
		return fmt.Errorf("keyPrefix must be configured to avoid root-level writes")
	case !c.PrivateRead:
		return fmt.Errorf("privateRead must remain enabled")
	case c.UploadPresignTTL <= 0 || c.UploadPresignTTL > 15*time.Minute:
		return fmt.Errorf("uploadPresignTTL must be between 1ns and 15m")
	case c.DownloadPresignTTL <= 0 || c.DownloadPresignTTL > 5*time.Minute:
		return fmt.Errorf("downloadPresignTTL must be between 1ns and 5m")
	case len(c.AllowedOrigins) == 0:
		return fmt.Errorf("allowedOrigins must be explicitly configured")
	case strings.TrimSpace(c.PublicReadBlockMode) == "":
		return fmt.Errorf("publicReadBlockMode is required")
	default:
		return nil
	}
}

func (c QueueConfig) RedactedSummary(ctx context.Context) (map[string]any, error) {
	password, err := c.Password.Resolve(ctx, ResolveOptions{})
	if err != nil {
		return nil, err
	}

	summary := map[string]any{
		"address":        c.Address,
		"db":             c.DB,
		"namespace":      c.Namespace,
		"maxConsumers":   c.MaxConsumers,
		"passwordSource": c.Password.Summary(),
	}

	if password != "" {
		summary["password"] = maskedLabel("redis")
	}

	return summary, nil
}

func (c ObjectStorageProviderConfig) RedactedSummary(ctx context.Context) (map[string]any, error) {
	if _, err := c.AccessKeyID.Resolve(ctx, ResolveOptions{}); err != nil {
		return nil, err
	}
	if _, err := c.AccessKeySecret.Resolve(ctx, ResolveOptions{}); err != nil {
		return nil, err
	}

	sessionToken := c.SessionToken.Summary()
	if !c.SessionToken.IsConfigured() {
		sessionToken = "unset"
	}

	return map[string]any{
		"name":                c.Name,
		"kind":                c.Kind,
		"bucket":              c.Bucket,
		"region":              c.Region,
		"endpoint":            c.Endpoint,
		"forcePathStyle":      c.ForcePathStyle,
		"keyPrefix":           c.KeyPrefix,
		"uploadPresignTTL":    c.UploadPresignTTL.String(),
		"downloadPresignTTL":  c.DownloadPresignTTL.String(),
		"privateRead":         c.PrivateRead,
		"corsConfigPath":      c.CORSConfigPath,
		"healthCheckURL":      c.HealthCheckURL,
		"publicReadBlockMode": c.PublicReadBlockMode,
		"accessKeyId":         c.AccessKeyID.Summary(),
		"accessKeySecret":     c.AccessKeySecret.Summary(),
		"sessionToken":        sessionToken,
		"allowedOrigins":      c.AllowedOrigins,
	}, nil
}

func (c AIProviderConfig) RedactedSummary(ctx context.Context) (map[string]any, error) {
	if _, err := c.Token.Resolve(ctx, ResolveOptions{}); err != nil {
		return nil, err
	}

	return map[string]any{
		"name":              c.Name,
		"kind":              c.Kind,
		"endpoint":          c.Endpoint,
		"model":             c.Model,
		"modelProfile":      c.ModelProfile,
		"models":            c.Models,
		"capabilities":      c.Capabilities,
		"timeout":           c.Timeout.String(),
		"allowRemote":       c.AllowRemote,
		"executionBoundary": c.ExecutionBoundary,
		"healthCheckURL":    c.HealthCheckURL,
		"token":             c.Token.Summary(),
	}, nil
}

func (c AIProviderConfig) ValidateLeastPrivilege() error {
	switch {
	case strings.TrimSpace(c.Name) == "":
		return fmt.Errorf("name is required")
	case strings.TrimSpace(c.Kind) == "":
		return fmt.Errorf("kind is required")
	case len(c.Capabilities) == 0:
		return fmt.Errorf("at least one capability is required")
	case c.Timeout <= 0 || c.Timeout > 2*time.Minute:
		return fmt.Errorf("timeout must be between 1ns and 2m")
	case strings.TrimSpace(c.ExecutionBoundary) == "":
		return fmt.Errorf("executionBoundary is required")
	case c.ExecutionBoundary == "remote-service" && !c.AllowRemote:
		return fmt.Errorf("remote-service boundary requires allowRemote=true")
	case strings.EqualFold(strings.TrimSpace(c.Kind), "openai-compatible") && strings.TrimSpace(c.Endpoint) == "":
		return fmt.Errorf("endpoint is required")
	case strings.EqualFold(strings.TrimSpace(c.Kind), "openai-compatible") && !c.Token.IsConfigured():
		return fmt.Errorf("token is required")
	case strings.TrimSpace(c.ModelProfile) == "":
		return fmt.Errorf("modelProfile is required")
	case len(c.Models) == 0:
		return fmt.Errorf("at least one model mapping is required")
	case strings.TrimSpace(c.Models[c.ModelProfile]) == "":
		return fmt.Errorf("modelProfile %q must map to a model", c.ModelProfile)
	default:
		return nil
	}
}

func maskConnectionString(dsn string) string {
	if dsn == "" {
		return dsn
	}

	if idx := strings.Index(dsn, "@"); idx > 0 {
		prefix := dsn[:idx]
		if credIdx := strings.LastIndex(prefix, ":"); credIdx >= 0 {
			return prefix[:credIdx+1] + "********" + dsn[idx:]
		}
	}

	return maskedLabel("dsn")
}

func (c *ObjectStorageProviderConfig) applyDefaults() {
	kind := strings.TrimSpace(c.Kind)
	if kind == "s3-compatible" && strings.TrimSpace(c.Region) == "" {
		c.Region = "us-east-1"
	}
	if kind == "tencent-cos" && strings.TrimSpace(c.Endpoint) == "" && strings.TrimSpace(c.Region) != "" {
		c.Endpoint = fmt.Sprintf("https://cos.%s.myqcloud.com", c.Region)
	}
	if strings.TrimSpace(c.HealthCheckURL) == "" && strings.TrimSpace(c.Endpoint) != "" {
		c.HealthCheckURL = c.Endpoint
	}
	if kind == "s3-compatible" {
		c.ForcePathStyle = true
	}
}

func (c *AIProviderConfig) applyDefaults() {
	if strings.TrimSpace(c.Kind) == "" {
		c.Kind = "deterministic"
	}
	if c.Timeout == 0 {
		c.Timeout = 20 * time.Second
	}
	if strings.TrimSpace(c.ModelProfile) == "" {
		c.ModelProfile = "default"
	}
	if len(c.Models) == 0 && strings.TrimSpace(c.Model) != "" {
		c.Models = map[string]string{
			c.ModelProfile: strings.TrimSpace(c.Model),
		}
	}
	if strings.TrimSpace(c.HealthCheckURL) == "" && strings.TrimSpace(c.Endpoint) != "" {
		c.HealthCheckURL = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/") + "/models"
	}
}
