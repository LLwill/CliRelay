package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScheduleUpdaterRefreshUsesDetachedTargetImageHelper(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "docker-compose.yml")
	envFile := filepath.Join(dir, ".env")
	logFile := filepath.Join(dir, "docker.log")
	dockerFile := filepath.Join(dir, "docker")
	if err := os.WriteFile(composeFile, []byte("services:\n  clirelay-updater:\n    image: ghcr.io/kittors/clirelay:latest\n"), 0o644); err != nil {
		t.Fatalf("write compose: %v", err)
	}
	if err := os.WriteFile(envFile, []byte("CLI_PROXY_IMAGE=ghcr.io/kittors/clirelay:latest\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(dockerFile, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_LOG\"\nprintf 'helper-id\\n'\n"), 0o755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_LOG", logFile)

	scheduled, err := scheduleUpdaterRefresh(
		context.Background(),
		composeFile,
		envFile,
		"cliproxy",
		"ghcr.io/kittors/clirelay:latest",
		17,
		updaterRunReporter{},
	)
	if err != nil {
		t.Fatalf("scheduleUpdaterRefresh failed: %v", err)
	}
	if !scheduled {
		t.Fatal("scheduled = false, want true")
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	command := string(data)
	for _, want := range []string{
		"run --rm -d --name clirelay-updater-refresh-17",
		"ghcr.io/kittors/clirelay:latest sh -c",
		"--force-recreate",
		"clirelay-updater",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("docker command missing %q: %s", want, command)
		}
	}
}
