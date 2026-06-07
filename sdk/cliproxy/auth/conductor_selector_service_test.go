package auth

import (
	"context"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestPickNextRouteGroupFallbacksToDefault(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &FillFirstSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			IncludeDefaultGroup: true,
			ChannelGroups: []internalconfig.RoutingChannelGroup{
				{
					Name:               "kimicode",
					ExcludeFromDefault: true,
					Match: internalconfig.ChannelGroupMatch{
						Channels: []string{"Kimi Channel"},
					},
				},
			},
		},
	})
	manager.RegisterExecutor(&stubExecutor{id: "codex"})

	for _, auth := range []*Auth{
		{
			ID:       "codex-default-auth",
			Label:    "Default Codex",
			Provider: "codex",
			Status:   StatusActive,
		},
		{
			ID:       "kimi-isolated-auth",
			Label:    "Kimi Channel",
			Provider: "kimi",
			Status:   StatusActive,
		},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}

	auth, executor, err := manager.pickNext(context.Background(), "codex", "", cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.RouteGroupMetadataKey:    "kimicode",
			cliproxyexecutor.RouteFallbackMetadataKey: "default",
		},
	}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("pickNext() error = %v", err)
	}
	if auth == nil || auth.ID != "codex-default-auth" {
		t.Fatalf("pickNext() auth = %#v, want codex-default-auth", auth)
	}
	if executor == nil || executor.Identifier() != "codex" {
		t.Fatalf("pickNext() executor = %#v, want codex executor", executor)
	}
}

func TestPickNextMixedRouteGroupFallbacksToDefaultWhenScopedExecutorMissing(t *testing.T) {
	t.Parallel()

	manager := NewManager(nil, &FillFirstSelector{}, nil)
	manager.SetConfig(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			IncludeDefaultGroup: true,
			ChannelGroups: []internalconfig.RoutingChannelGroup{
				{
					Name:               "kimicode",
					ExcludeFromDefault: true,
					Match: internalconfig.ChannelGroupMatch{
						Channels: []string{"Kimi Channel"},
					},
				},
			},
		},
	})
	manager.RegisterExecutor(&stubExecutor{id: "codex"})

	for _, auth := range []*Auth{
		{
			ID:       "codex-default-auth",
			Label:    "Default Codex",
			Provider: "codex",
			Status:   StatusActive,
		},
		{
			ID:       "kimi-isolated-auth",
			Label:    "Kimi Channel",
			Provider: "kimi",
			Status:   StatusActive,
		},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register %s: %v", auth.ID, err)
		}
	}

	auth, executor, provider, err := manager.pickNextMixed(context.Background(), []string{"kimi", "codex"}, "", cliproxyexecutor.Options{
		Metadata: map[string]any{
			cliproxyexecutor.RouteGroupMetadataKey:    "kimicode",
			cliproxyexecutor.RouteFallbackMetadataKey: "default",
		},
	}, map[string]struct{}{})
	if err != nil {
		t.Fatalf("pickNextMixed() error = %v", err)
	}
	if provider != "codex" {
		t.Fatalf("pickNextMixed() provider = %q, want codex", provider)
	}
	if auth == nil || auth.ID != "codex-default-auth" {
		t.Fatalf("pickNextMixed() auth = %#v, want codex-default-auth", auth)
	}
	if executor == nil || executor.Identifier() != "codex" {
		t.Fatalf("pickNextMixed() executor = %#v, want codex executor", executor)
	}
}
