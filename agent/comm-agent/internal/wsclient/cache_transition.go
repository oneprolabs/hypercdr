package wsclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hypercdr-platform/agent/comm-agent/internal/kube"
	"hypercdr-platform/agent/comm-agent/pkg/protocol"
)

type cacheTransitionRuntime interface {
	ConfigureCache(context.Context, string, string, string, string, bool, int, bool, int, bool) error
	ValidateCacheDirectoryTransition(context.Context, string, string, []string, []string) error
	RunCacheDirectoryStage(context.Context, string, string, string, []string, []string) error
}

func applyCacheTransition(ctx context.Context, runtime cacheTransitionRuntime, command protocol.CacheSettingsCommand) (bool, error) {
	state := cacheTransitionState{}
	return resumeCacheTransition(ctx, runtime, command, state, func(next cacheTransitionState) error { state = next; return nil })
}

type cacheTransitionState struct {
	Phase   string `json:"phase"`
	Failure string `json:"failure,omitempty"`
}

func (c *Client) cacheTransitionPending() bool {
	for _, record := range c.ledger.recoverableRecords() {
		if record.Type == "cache-settings" {
			return true
		}
	}
	return false
}

func resumeCacheTransition(ctx context.Context, runtime cacheTransitionRuntime, command protocol.CacheSettingsCommand, state cacheTransitionState, persist func(cacheTransitionState) error) (bool, error) {
	transition := command.Transition
	if transition == nil {
		return true, fmt.Errorf("cache transition is missing")
	}
	advance := func(phase string, failure error) error {
		next := state
		next.Phase = phase
		if failure != nil {
			next.Failure = failure.Error()
		}
		if err := persist(next); err != nil {
			return err
		}
		state = next
		return nil
	}
	configure := func(ctx context.Context, settings protocol.CacheConfigurationSnapshot) error {
		return runtime.ConfigureCache(ctx, command.Namespace, settings.RootPath, settings.IndexPath, settings.DrillPath, settings.IndexEnabled, settings.IndexMaxGiB, settings.DrillEnabled, settings.DrillMaxGiB, false)
	}
	desired := protocol.CacheConfigurationSnapshot{RootPath: command.RootPath, IndexPath: command.IndexPath, DrillPath: command.DrillPath, IndexEnabled: command.IndexEnabled, IndexMaxGiB: command.IndexMaxGiB, DrillEnabled: command.DrillEnabled, DrillMaxGiB: command.DrillMaxGiB}
	if state.Phase == "" {
		if err := runtime.ValidateCacheDirectoryTransition(ctx, command.Namespace, transition.IsolationID, transition.Paths, transition.Nodes); err != nil {
			return true, err
		}
		if err := advance("applying", nil); err != nil {
			return true, err
		}
	}
	if state.Phase == "applying" {
		if err := configure(ctx, desired); err != nil {
			if saveErr := advance("rollback-configuration", err); saveErr != nil {
				return false, errors.Join(err, saveErr)
			}
		} else if err := advance("isolating", nil); err != nil {
			return false, err
		}
	}
	if state.Phase == "isolating" {
		if err := runtime.RunCacheDirectoryStage(ctx, command.Namespace, "isolate", transition.IsolationID, transition.Paths, transition.Nodes); err != nil {
			if !errors.Is(err, kube.ErrCacheDirectoryStageFailed) {
				return false, fmt.Errorf("cache isolation state requires recovery before rollback: %w", err)
			}
			if saveErr := advance("rollback-directories", err); saveErr != nil {
				return false, errors.Join(err, saveErr)
			}
		} else if err := advance("isolated", nil); err != nil {
			return false, err
		}
	}
	if state.Phase == "rollback-directories" || state.Phase == "rollback-configuration" {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()
		if state.Phase == "rollback-directories" {
			if err := runtime.RunCacheDirectoryStage(rollbackCtx, command.Namespace, "rollback", transition.IsolationID, transition.Paths, transition.Nodes); err != nil {
				return false, err
			}
			if err := advance("rollback-configuration", nil); err != nil {
				return false, err
			}
		}
		if err := configure(rollbackCtx, transition.Previous); err != nil {
			return false, err
		}
		if err := advance("rolled-back", nil); err != nil {
			return false, err
		}
	}
	switch state.Phase {
	case "isolated":
		return true, nil
	case "rolled-back":
		return true, fmt.Errorf("cache settings rolled back: %s", state.Failure)
	default:
		return false, fmt.Errorf("unknown cache transition phase %q", state.Phase)
	}
}
