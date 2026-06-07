package serviceapp

import (
	"fmt"
	"os"

	log "github.com/sirupsen/logrus"
)

type ServerStarter interface {
	Start() error
}

func StartServerLoop(server ServerStarter) chan error {
	if server == nil {
		return nil
	}
	serverErr := make(chan error, 1)
	go func() {
		if errStart := server.Start(); errStart != nil {
			serverErr <- errStart
		} else {
			serverErr <- nil
		}
	}()
	return serverErr
}

func EnsureAuthDir(authDir string) error {
	info, err := os.Stat(authDir)
	if err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.MkdirAll(authDir, 0o755); mkErr != nil {
				return fmt.Errorf("cliproxy: failed to create auth directory %s: %w", authDir, mkErr)
			}
			log.Infof("created missing auth directory: %s", authDir)
			return nil
		}
		return fmt.Errorf("cliproxy: error checking auth directory %s: %w", authDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cliproxy: auth path exists but is not a directory: %s", authDir)
	}
	return nil
}
