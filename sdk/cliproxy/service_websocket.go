package cliproxy

import (
	"context"
	"net/http"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	serviceapp "github.com/router-for-me/CLIProxyAPI/v6/sdkbridge/service"
	log "github.com/sirupsen/logrus"
)

type websocketGateway interface {
	Path() string
	Handler() http.Handler
	Stop(ctx context.Context) error
	RelayValue() any
}

func newDefaultWebsocketGateway(s *Service) websocketGateway {
	if s == nil {
		return nil
	}
	return serviceapp.NewDefaultWebsocketGateway(s.wsOnConnected, s.wsOnDisconnected)
}

func (s *Service) ensureWebsocketGateway() {
	if s == nil {
		return
	}
	if s.wsGateway != nil {
		return
	}
	s.wsGateway = newDefaultWebsocketGateway(s)
}

func (s *Service) wsOnConnected(channelID string) {
	if s == nil || channelID == "" {
		return
	}
	if !strings.HasPrefix(strings.ToLower(channelID), "aistudio-") {
		return
	}
	if s.coreManager != nil {
		if existing, ok := s.coreManager.GetByID(channelID); ok && existing != nil {
			if !existing.Disabled && existing.Status == coreauth.StatusActive {
				return
			}
		}
	}
	now := time.Now().UTC()
	auth := &coreauth.Auth{
		ID:         channelID,
		Provider:   "aistudio",
		Label:      channelID,
		Status:     coreauth.StatusActive,
		CreatedAt:  now,
		UpdatedAt:  now,
		Attributes: map[string]string{"runtime_only": "true"},
		Metadata:   map[string]any{"email": channelID},
	}
	log.Infof("websocket provider connected: %s", channelID)
	s.emitAuthUpdate(context.WithoutCancel(context.Background()), runtimeAuthUpdate{
		Action: runtimeAuthUpdateActionAdd,
		ID:     auth.ID,
		Auth:   auth,
	})
}

func (s *Service) wsOnDisconnected(channelID string, reason error) {
	if s == nil || channelID == "" {
		return
	}
	if reason != nil {
		if strings.Contains(reason.Error(), "replaced by new connection") {
			log.Infof("websocket provider replaced: %s", channelID)
			return
		}
		log.Warnf("websocket provider disconnected: %s (%v)", channelID, reason)
	} else {
		log.Infof("websocket provider disconnected: %s", channelID)
	}
	s.emitAuthUpdate(context.WithoutCancel(context.Background()), runtimeAuthUpdate{
		Action: runtimeAuthUpdateActionDelete,
		ID:     channelID,
	})
}

func (s *Service) bindWebsocketGateway(ctx context.Context) {
	if s == nil {
		return
	}
	s.ensureWebsocketGateway()
	if s.server == nil || s.wsGateway == nil {
		return
	}
	s.server.AttachWebsocketRoute(s.wsGateway.Path(), s.wsGateway.Handler())
	s.server.SetWebsocketAuthChangeHandler(func(oldEnabled, newEnabled bool) {
		if oldEnabled == newEnabled {
			return
		}
		if !oldEnabled && newEnabled {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if errStop := s.wsGateway.Stop(stopCtx); errStop != nil {
				log.Warnf("failed to reset websocket connections after ws-auth change %t -> %t: %v", oldEnabled, newEnabled, errStop)
				return
			}
			log.Debugf("ws-auth enabled; existing websocket sessions terminated to enforce authentication")
			return
		}
		log.Debugf("ws-auth disabled; existing websocket sessions remain connected")
	})
}
