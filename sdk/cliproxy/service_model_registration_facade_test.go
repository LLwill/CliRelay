package cliproxy

import (
	"context"
	"testing"
	"time"
)

type facadeRegisteredCall struct {
	provider string
	clientID string
	models   []*ModelInfo
}

type facadeUnregisteredCall struct {
	provider string
	clientID string
}

type facadeRegistryHook struct {
	registeredCh   chan facadeRegisteredCall
	unregisteredCh chan facadeUnregisteredCall
}

func (h *facadeRegistryHook) OnModelsRegistered(ctx context.Context, provider, clientID string, models []*ModelInfo) {
	h.registeredCh <- facadeRegisteredCall{provider: provider, clientID: clientID, models: models}
}

func (h *facadeRegistryHook) OnModelsUnregistered(ctx context.Context, provider, clientID string) {
	h.unregisteredCh <- facadeUnregisteredCall{provider: provider, clientID: clientID}
}

func TestSetGlobalModelRegistryHookDelegatesToSharedRegistry(t *testing.T) {
	reg := GlobalModelRegistry()
	if reg == nil {
		t.Fatal("GlobalModelRegistry returned nil")
	}

	clientID := "sdk-facade-hook-client"
	reg.UnregisterClient(clientID)
	t.Cleanup(func() {
		reg.UnregisterClient(clientID)
		SetGlobalModelRegistryHook(nil)
	})

	hook := &facadeRegistryHook{
		registeredCh:   make(chan facadeRegisteredCall, 1),
		unregisteredCh: make(chan facadeUnregisteredCall, 1),
	}
	SetGlobalModelRegistryHook(hook)

	reg.RegisterClient(clientID, "OpenAI", []*ModelInfo{{ID: "sdk-facade-model", DisplayName: "SDK Facade Model"}})

	select {
	case call := <-hook.registeredCh:
		if call.provider != "openai" {
			t.Fatalf("registered provider = %q, want openai", call.provider)
		}
		if call.clientID != clientID {
			t.Fatalf("registered clientID = %q, want %q", call.clientID, clientID)
		}
		if len(call.models) != 1 || call.models[0] == nil || call.models[0].ID != "sdk-facade-model" {
			t.Fatalf("registered models = %#v, want single sdk-facade-model", call.models)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OnModelsRegistered hook call")
	}

	reg.UnregisterClient(clientID)

	select {
	case call := <-hook.unregisteredCh:
		if call.provider != "openai" {
			t.Fatalf("unregistered provider = %q, want openai", call.provider)
		}
		if call.clientID != clientID {
			t.Fatalf("unregistered clientID = %q, want %q", call.clientID, clientID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for OnModelsUnregistered hook call")
	}
}
