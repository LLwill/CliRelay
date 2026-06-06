package callback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type Forwarder struct {
	server *http.Server
	done   chan struct{}
}

var (
	forwardersMu sync.Mutex
	forwarders   = make(map[int]*Forwarder)
)

func Start(port int, provider, targetBase string) (*Forwarder, error) {
	forwarder, _, err := startOnExactPort(port, provider, targetBase)
	return forwarder, err
}

func StartOnAvailablePort(preferredPort int, provider, targetBase string) (*Forwarder, int, error) {
	forwarder, port, err := startOnExactPort(preferredPort, provider, targetBase)
	if err == nil {
		return forwarder, port, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, 0, err
	}
	log.WithError(err).Warnf("callback forwarder for %s could not listen on preferred port %d, trying a free port", provider, preferredPort)
	return startOnExactPort(0, provider, targetBase)
}

func startOnExactPort(port int, provider, targetBase string) (*Forwarder, int, error) {
	forwardersMu.Lock()
	prev := forwarders[port]
	if prev != nil {
		delete(forwarders, port)
	}
	forwardersMu.Unlock()

	if prev != nil {
		stopInstance(context.Background(), port, prev)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	actualPort := port
	if tcpAddr, ok := ln.Addr().(*net.TCPAddr); ok && tcpAddr != nil {
		actualPort = tcpAddr.Port
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := targetBase
		if raw := r.URL.RawQuery; raw != "" {
			if strings.Contains(target, "?") {
				target = target + "&" + raw
			} else {
				target = target + "?" + raw
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target, http.StatusFound)
	})

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	done := make(chan struct{})

	go func() {
		if errServe := srv.Serve(ln); errServe != nil && !errors.Is(errServe, http.ErrServerClosed) {
			log.WithError(errServe).Warnf("callback forwarder for %s stopped unexpectedly", provider)
		}
		close(done)
	}()

	forwarder := &Forwarder{
		server: srv,
		done:   done,
	}

	forwardersMu.Lock()
	forwarders[actualPort] = forwarder
	forwardersMu.Unlock()

	log.Infof("callback forwarder for %s listening on %s", provider, ln.Addr().String())

	return forwarder, actualPort, nil
}

func Stop(port int) {
	forwardersMu.Lock()
	forwarder := forwarders[port]
	if forwarder != nil {
		delete(forwarders, port)
	}
	forwardersMu.Unlock()

	stopInstance(context.Background(), port, forwarder)
}

func StopInstance(ctx context.Context, port int, forwarder *Forwarder) {
	if forwarder == nil {
		return
	}
	forwardersMu.Lock()
	if current := forwarders[port]; current == forwarder {
		delete(forwarders, port)
	}
	forwardersMu.Unlock()

	stopInstance(ctx, port, forwarder)
}

func stopInstance(ctx context.Context, port int, forwarder *Forwarder) {
	if forwarder == nil || forwarder.server == nil {
		return
	}

	parentCtx := ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, 2*time.Second)
	defer cancel()

	if err := forwarder.server.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.WithError(err).Warnf("failed to shut down callback forwarder on port %d", port)
	}

	select {
	case <-forwarder.done:
	case <-time.After(2 * time.Second):
	}

	log.Infof("callback forwarder on port %d stopped", port)
}
