package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/provider/storage"
)

type Probe func(ctx context.Context) error

type RegisteredProbe struct {
	Name  string
	Probe Probe
}

type Checker struct {
	Probes []RegisteredProbe
}

type Status struct {
	Name      string    `json:"name"`
	Healthy   bool      `json:"healthy"`
	Detail    string    `json:"detail,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

func NewDefault(cfg config.AppConfig) Checker {
	probes := []RegisteredProbe{
		{
			Name:  "postgres",
			Probe: TCPProbe(cfg.Database.Address(), 2*time.Second),
		},
		{
			Name:  "redis",
			Probe: TCPProbe(cfg.Queue.Address, 2*time.Second),
		},
	}

	for _, providerCfg := range append([]config.ObjectStorageProviderConfig{cfg.StorageProviders.Primary}, cfg.StorageProviders.Backup...) {
		probes = append(probes, RegisteredProbe{
			Name:  fmt.Sprintf("storage-%s", providerCfg.Name),
			Probe: StorageProbe(providerCfg, 5*time.Second),
		})
	}

	return Checker{Probes: probes}
}

func (c Checker) Check(ctx context.Context) []Status {
	results := make([]Status, 0, len(c.Probes))
	for _, probe := range c.Probes {
		status := Status{
			Name:      probe.Name,
			Healthy:   true,
			CheckedAt: time.Now().UTC(),
		}
		if err := probe.Probe(ctx); err != nil {
			status.Healthy = false
			status.Detail = err.Error()
		}
		results = append(results, status)
	}

	return results
}

func OverallStatus(results []Status) string {
	state := "ok"
	for _, result := range results {
		if !result.Healthy {
			if result.Detail == "" {
				return "failed"
			}
			state = "degraded"
		}
	}

	return state
}

func TCPProbe(address string, timeout time.Duration) Probe {
	return func(ctx context.Context) error {
		dialer := net.Dialer{Timeout: timeout}
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		_ = conn.Close()
		return nil
	}
}

func HTTPProbe(rawURL string, timeout time.Duration) Probe {
	client := http.Client{Timeout: timeout}

	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode >= http.StatusBadRequest {
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		return nil
	}
}

func StorageProbe(cfg config.ObjectStorageProviderConfig, timeout time.Duration) Probe {
	return func(ctx context.Context) error {
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return storage.ValidateProvider(probeCtx, cfg)
	}
}
