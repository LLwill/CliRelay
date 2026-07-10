package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const updaterComposeServiceName = "clirelay-updater"

// scheduleUpdaterRefresh starts a detached helper from the newly pulled image.
// The helper survives replacement of this updater container, recreates the
// sidecar, and lets the new process restore the completed status snapshot.
func scheduleUpdaterRefresh(ctx context.Context, composeFile string, envFile string, projectName string, imageRef string, runID uint64, reporter updateReporter) (bool, error) {
	if strings.TrimSpace(composeFile) == "" || !composeFileHasService(composeFile, updaterComposeServiceName) {
		return false, nil
	}
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return false, fmt.Errorf("cannot refresh updater sidecar without a target image")
	}
	projectDir := filepath.Dir(composeFile)
	composeArgs := buildComposeArgs(composeFile, envFile, projectName, "up", "-d", "--no-deps", "--force-recreate", updaterComposeServiceName)
	quotedArgs := make([]string, 0, len(composeArgs))
	for _, arg := range composeArgs {
		quotedArgs = append(quotedArgs, shellQuote(arg))
	}
	command := "sleep 2; exec docker " + strings.Join(quotedArgs, " ")
	helperName := "clirelay-updater-refresh-" + strconv.FormatUint(runID, 10)

	reporter.Stage("finalizing", "scheduling updater sidecar refresh")
	cmd := exec.CommandContext(
		ctx,
		"docker",
		"run",
		"--rm",
		"-d",
		"--name",
		helperName,
		"-v",
		"/var/run/docker.sock:/var/run/docker.sock",
		"-v",
		projectDir+":"+projectDir,
		"-w",
		projectDir,
		imageRef,
		"sh",
		"-c",
		command,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("schedule updater sidecar refresh: %w: %s", err, strings.TrimSpace(string(output)))
	}
	reporter.Log("stdout", "scheduled updater sidecar refresh helper "+strings.TrimSpace(string(output)))
	return true, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
