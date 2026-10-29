package app

import (
	"context"
	"time"

	"github.com/nelvko/lazyclash/internal/mihomo"
)

// RuleSnapshot describes the rules returned by the running core. It never
// substitutes a saved profile or a previous successful response for live data.
type RuleSnapshot struct {
	State      string
	Rules      []mihomo.Rule
	ObservedAt time.Time
	Error      string
}

func (a *App) Rules(ctx context.Context) (RuleSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var before RuntimeSnapshot
	var client mihomo.Client
	var identity string
	err := a.locked(ctx, func() error {
		var err error
		before, client, identity, err = a.inspectRuntimeLocked(ctx)
		return err
	})
	s := RuleSnapshot{State: before.State, Error: before.Error, ObservedAt: before.ObservedAt}
	if err != nil || s.State != "ready" {
		return s, err
	}
	// HTTP runs without the application lock so stopping the service or leaving
	// the page can interrupt a slow request. Recheck the owner and process before
	// returning a successful list, just as with runtime sampling.
	rules, queryErr := client.Rules(ctx)
	if ctx.Err() != nil {
		return s, ctx.Err()
	}
	err = a.locked(ctx, func() error {
		current, _, currentID, err := a.inspectRuntimeLocked(ctx)
		if err != nil {
			return err
		}
		s.State, s.Error, s.ObservedAt = current.State, current.Error, time.Now()
		if current.State != "ready" {
			return nil
		}
		if currentID != identity || before.Service.PID != current.Service.PID {
			s.State, s.Error = "unavailable", "Core changed while loading rules; refresh to read its current rules"
			return nil
		}
		if queryErr != nil {
			s.State, s.Error = "unavailable", queryErr.Error()
			return nil
		}
		s.Rules = rules
		return nil
	})
	return s, err
}
