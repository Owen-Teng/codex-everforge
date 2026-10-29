package resetcloud

import (
	"codex-quota-monitor/internal/xactivity"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Listen          string           `json:"listen"`
	StateFile       string           `json:"stateFile"`
	SecretFile      string           `json:"secretFile"`
	AuthorizationID string           `json:"authorizationId"`
	Deadline        int64            `json:"deadline"`
	X               xactivity.Config `json:"x"`
}

func ReadConfig(reader io.Reader) (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", StateFile: "/var/lib/reset-cloud/state.json", SecretFile: "/run/secrets/x-bearer", X: xactivity.DefaultConfig()}
	raw, err := io.ReadAll(io.LimitReader(reader, 16385))
	if err != nil || len(raw) > 16384 {
		return Config{}, errors.New("invalid_config")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF || !filepath.IsAbs(c.StateFile) || !filepath.IsAbs(c.SecretFile) {
		return Config{}, errors.New("invalid_config")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Config{}, errors.New("invalid_config")
	}
	if x, ok := fields["x"]; ok {
		c.X, err = xactivity.ReadConfig(strings.NewReader(string(x)))
		if err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

// RunAuthorized is a single finite authorization, never a restart/renew loop.
// The service remains available when this goroutine returns.
func (s *Service) RunAuthorized(parent context.Context, c Config) {
	if !c.X.Enabled {
		s.Stop("disabled")
		return
	}
	if !c.X.BillingApproved || !c.X.BillingCycleLimitConfirmed {
		s.Stop("billing_not_authorized")
		return
	}
	if err := c.X.ValidateObservationPreflight(); err != nil {
		s.Stop(err.Error())
		return
	}
	if err := s.ReserveGrant(c.AuthorizationID, c.Deadline, c.X); err != nil {
		s.Stop(err.Error())
		return
	}
	ctx, cancel := context.WithDeadline(parent, time.Unix(c.Deadline, 0))
	defer cancel()
	connector, err := xactivity.NewObservationConnector(c.X, xactivity.SecretFileReader{Path: c.SecretFile})
	if err != nil {
		s.Stop("connector_not_configured")
		return
	}
	defer connector.Close()
	resolved, err := connector.Preflight(ctx)
	if err != nil {
		s.Stop("upstream_preflight_failed")
		return
	}
	processor, err := xactivity.NewProcessor(resolved, time.Now, "")
	if err != nil {
		s.Stop("processor_unavailable")
		return
	}
	runner := xactivity.Runner{Config: resolved, Connector: connector, Processor: processor, Observation: true,
		OnStatus: func(status xactivity.Status) {
			if s.RecordStatus(c.AuthorizationID, status) != nil {
				cancel()
			}
		},
		OnDecision: func(d xactivity.Decision) {
			if s.Ingest(d) != nil {
				cancel()
				s.Stop("event_save_failed")
			}
		},
	}
	runner.Run(ctx)
}
