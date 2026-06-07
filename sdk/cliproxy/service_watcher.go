package cliproxy

import (
	"context"
	"fmt"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	serviceapp "github.com/router-for-me/CLIProxyAPI/v6/sdkbridge/service"
	log "github.com/sirupsen/logrus"
)

// WatcherFactory creates a watcher for configuration and token changes.
// The reload callback receives the updated configuration when changes are detected.
type WatcherFactory func(configPath, authDir string, reload func(*config.Config)) (*WatcherWrapper, error)

// WatcherWrapper exposes the subset of watcher methods required by the SDK.
type WatcherWrapper struct {
	start func(ctx context.Context) error
	stop  func() error

	setConfig             func(cfg *config.Config)
	snapshotAuths         func() []*coreauth.Auth
	setUpdateQueue        func(queue chan<- runtimeAuthUpdate)
	dispatchRuntimeUpdate func(update runtimeAuthUpdate) bool
}

// Start proxies to the underlying watcher Start implementation.
func (w *WatcherWrapper) Start(ctx context.Context) error {
	if w == nil || w.start == nil {
		return nil
	}
	return w.start(ctx)
}

// Stop proxies to the underlying watcher Stop implementation.
func (w *WatcherWrapper) Stop() error {
	if w == nil || w.stop == nil {
		return nil
	}
	return w.stop()
}

// SetConfig updates the watcher configuration cache.
func (w *WatcherWrapper) SetConfig(cfg *config.Config) {
	if w == nil || w.setConfig == nil {
		return
	}
	w.setConfig(cfg)
}

// DispatchRuntimeAuthUpdate forwards runtime auth updates into the watcher-managed queue.
func (w *WatcherWrapper) DispatchRuntimeAuthUpdate(update runtimeAuthUpdate) bool {
	if w == nil || w.dispatchRuntimeUpdate == nil {
		return false
	}
	return w.dispatchRuntimeUpdate(update)
}

// SnapshotAuths returns the current auth entries derived from legacy clients.
func (w *WatcherWrapper) SnapshotAuths() []*coreauth.Auth {
	if w == nil || w.snapshotAuths == nil {
		return nil
	}
	return w.snapshotAuths()
}

// SetAuthUpdateQueue registers the channel used to propagate auth updates.
func (w *WatcherWrapper) SetAuthUpdateQueue(queue chan<- runtimeAuthUpdate) {
	if w == nil || w.setUpdateQueue == nil {
		return
	}
	w.setUpdateQueue(queue)
}

func defaultWatcherFactory(configPath, authDir string, reload func(*config.Config)) (*WatcherWrapper, error) {
	w, err := serviceapp.NewWatcher(configPath, authDir, reload)
	if err != nil {
		return nil, err
	}

	return &WatcherWrapper{
		start: func(ctx context.Context) error {
			return w.Start(ctx)
		},
		stop: func() error {
			return w.Stop()
		},
		setConfig: func(cfg *config.Config) {
			w.SetConfig(cfg)
		},
		snapshotAuths: func() []*coreauth.Auth { return w.SnapshotCoreAuths() },
		setUpdateQueue: func(queue chan<- runtimeAuthUpdate) {
			if queue == nil {
				w.SetAuthUpdateQueue(nil)
				return
			}
			bridgeQueue := make(chan serviceapp.RuntimeAuthUpdate, 256)
			w.SetAuthUpdateQueue(bridgeQueue)
			go func() {
				for update := range bridgeQueue {
					queue <- fromBridgeRuntimeAuthUpdate(update)
				}
			}()
		},
		dispatchRuntimeUpdate: func(update runtimeAuthUpdate) bool {
			return w.DispatchRuntimeAuthUpdate(toBridgeRuntimeAuthUpdate(update))
		},
	}, nil
}

func (s *Service) startWatcher(ctx context.Context) error {
	if s == nil {
		return nil
	}
	reloadCallback := func(newCfg *config.Config) {
		s.applyConfigReload(newCfg, false)
	}

	watcherWrapper, err := s.watcherFactory(s.configPath, s.cfg.AuthDir, reloadCallback)
	if err != nil {
		return fmt.Errorf("cliproxy: failed to create watcher: %w", err)
	}
	s.watcher = watcherWrapper
	s.ensureAuthUpdateQueue(ctx)
	if s.authUpdates != nil {
		watcherWrapper.SetAuthUpdateQueue(s.authUpdates)
	}
	watcherWrapper.SetConfig(s.cfg)

	watcherCtx, watcherCancel := context.WithCancel(ctx)
	s.watcherCancel = watcherCancel
	if err = watcherWrapper.Start(watcherCtx); err != nil {
		return fmt.Errorf("cliproxy: failed to start watcher: %w", err)
	}
	log.Info("file watcher started for config and auth directory changes")
	return nil
}
