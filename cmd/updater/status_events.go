package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const updaterEventHeartbeat = 15 * time.Second

func resolveUpdaterStateFile(explicit string) string {
	return strings.TrimSpace(explicit)
}

func (s *updaterServer) restoreStatus() {
	if s == nil || strings.TrimSpace(s.stateFile) == "" {
		return
	}
	data, err := os.ReadFile(s.stateFile)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		log.Printf("clirelay updater: read state failed: %v", err)
		return
	}
	var status updateStatusResponse
	if err := json.Unmarshal(data, &status); err != nil {
		log.Printf("clirelay updater: decode state failed: %v", err)
		return
	}
	if strings.TrimSpace(status.Status) == "" || strings.TrimSpace(status.Stage) == "" {
		return
	}
	s.status = cloneUpdateStatus(status)
	s.runID = status.RunID
	if strings.EqualFold(status.Status, "running") {
		now := time.Now().UTC().Format(time.RFC3339)
		s.status.Status = "failed"
		s.status.Stage = "failed"
		s.status.MessageCode = "updater_restarted"
		s.status.Message = "updater restarted before the update completed"
		s.status.UpdatedAt = now
		s.status.FinishedAt = now
		s.statusChangedLocked()
	}
}

func (s *updaterServer) persistStatusLocked() {
	if s == nil || strings.TrimSpace(s.stateFile) == "" {
		return
	}
	data, err := json.MarshalIndent(s.status, "", "  ")
	if err != nil {
		log.Printf("clirelay updater: encode state failed: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.stateFile), 0o755); err != nil {
		log.Printf("clirelay updater: create state directory failed: %v", err)
		return
	}
	tmp := s.stateFile + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		log.Printf("clirelay updater: write state failed: %v", err)
		return
	}
	if err := os.Rename(tmp, s.stateFile); err != nil {
		_ = os.Remove(tmp)
		log.Printf("clirelay updater: replace state failed: %v", err)
	}
}

func cloneUpdateStatus(status updateStatusResponse) updateStatusResponse {
	clone := status
	if len(status.Logs) > 0 {
		clone.Logs = append([]updateLogEntry(nil), status.Logs...)
	}
	return clone
}

func (s *updaterServer) statusChangedLocked() {
	s.publishStatusLocked(true)
}

func (s *updaterServer) statusObservedLocked() {
	s.publishStatusLocked(false)
}

func (s *updaterServer) publishStatusLocked(persist bool) {
	s.status.EventID++
	if persist {
		s.persistStatusLocked()
	}
	snapshot := cloneUpdateStatus(s.status)
	for subscriber := range s.subscribers {
		select {
		case subscriber <- snapshot:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- snapshot:
			default:
			}
		}
	}
}

func (s *updaterServer) startUpdate(service string, req updateRequest) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.EqualFold(s.status.Status, "running") {
		return s.runID, false
	}
	s.runID++
	eventID := s.status.EventID
	now := time.Now().UTC().Format(time.RFC3339)
	s.status = updateStatusResponse{
		RunID:              s.runID,
		EventID:            eventID,
		Status:             "running",
		Stage:              "preparing",
		MessageCode:        "preparing_deployment",
		Message:            "preparing deployment configuration",
		Service:            service,
		CurrentVersion:     strings.TrimSpace(req.CurrentVersion),
		CurrentCommit:      strings.TrimSpace(req.CurrentCommit),
		CurrentUIVersion:   strings.TrimSpace(req.CurrentUIVersion),
		CurrentUICommit:    strings.TrimSpace(req.CurrentUICommit),
		TargetImage:        strings.TrimSpace(req.Image),
		TargetTag:          strings.TrimSpace(req.Tag),
		TargetVersion:      strings.TrimSpace(req.Version),
		TargetCommit:       strings.TrimSpace(req.Commit),
		TargetCommitURL:    strings.TrimSpace(req.CommitURL),
		TargetUIVersion:    strings.TrimSpace(req.UIVersion),
		TargetUICommit:     strings.TrimSpace(req.UICommit),
		TargetUICommitURL:  strings.TrimSpace(req.UICommitURL),
		TargetChannel:      strings.TrimSpace(req.Channel),
		ReleaseName:        strings.TrimSpace(req.ReleaseName),
		ReleaseTag:         strings.TrimSpace(req.ReleaseTag),
		ReleaseNotes:       strings.TrimSpace(req.ReleaseNotes),
		ReleaseURL:         strings.TrimSpace(req.ReleaseURL),
		ReleasePublishedAt: strings.TrimSpace(req.ReleasePublishedAt),
		StartedAt:          now,
		UpdatedAt:          now,
	}
	s.pullSkipped = false
	s.pullSkipLog = ""
	s.statusChangedLocked()
	return s.runID, true
}

func (s *updaterServer) appendLog(runID uint64, stream string, message string) {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.runID {
		return
	}
	if s.status.Stage == "pulling" && strings.Contains(trimmed, "Skipped") {
		s.pullSkipped = true
		if s.pullSkipLog == "" {
			s.pullSkipLog = trimmed
		}
	}
	s.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.status.Logs = append(s.status.Logs, updateLogEntry{
		Timestamp: s.status.UpdatedAt,
		Stream:    strings.TrimSpace(stream),
		Message:   trimmed,
	})
	if len(s.status.Logs) > maxUpdateLogEntries {
		s.status.Logs = append([]updateLogEntry(nil), s.status.Logs[len(s.status.Logs)-maxUpdateLogEntries:]...)
	}
	s.statusObservedLocked()
}

func (s *updaterServer) updateStage(runID uint64, stage string, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.runID {
		return
	}
	s.status.Stage = strings.TrimSpace(stage)
	s.status.MessageCode = strings.TrimSpace(stage)
	s.status.Message = strings.TrimSpace(message)
	s.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.statusChangedLocked()
}

func (s *updaterServer) updateProgress(runID uint64, stage string, messageCode string, message string, current int, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.runID {
		return
	}
	if current < 0 {
		current = 0
	}
	if total < 0 {
		total = 0
	}
	if total > 0 && current > total {
		current = total
	}
	s.status.Stage = strings.TrimSpace(stage)
	s.status.MessageCode = strings.TrimSpace(messageCode)
	s.status.Message = strings.TrimSpace(message)
	s.status.ProgressCurrent = current
	s.status.ProgressTotal = total
	if total > 0 {
		s.status.ProgressUnit = "steps"
		s.status.ProgressPercent = float64(current) * 100 / float64(total)
	} else {
		s.status.ProgressUnit = ""
		s.status.ProgressPercent = 0
	}
	s.status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.statusChangedLocked()
}

func (s *updaterServer) finishUpdate(runID uint64, status string, stage string, messageCode string, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runID != s.runID {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.status.Status = strings.TrimSpace(status)
	s.status.Stage = strings.TrimSpace(stage)
	s.status.MessageCode = strings.TrimSpace(messageCode)
	s.status.Message = strings.TrimSpace(message)
	if status == "completed" {
		if s.status.ProgressTotal > 0 {
			s.status.ProgressCurrent = s.status.ProgressTotal
		}
		s.status.ProgressPercent = 100
	}
	s.status.UpdatedAt = now
	s.status.FinishedAt = now
	s.statusChangedLocked()
}

func (s *updaterServer) snapshot() updateStatusResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneUpdateStatus(s.status)
}

type updaterRunReporter struct {
	server *updaterServer
	runID  uint64
}

func (r updaterRunReporter) Stage(stage string, message string) {
	if r.server == nil {
		return
	}
	r.server.updateStage(r.runID, stage, message)
}

func (r updaterRunReporter) Progress(stage string, messageCode string, message string, current int, total int) {
	if r.server == nil {
		return
	}
	r.server.updateProgress(r.runID, stage, messageCode, message, current, total)
}

func (r updaterRunReporter) Log(stream string, message string) {
	if r.server == nil {
		return
	}
	r.server.appendLog(r.runID, stream, message)
}

func (s *updaterServer) subscribeStatus() (<-chan updateStatusResponse, updateStatusResponse, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan updateStatusResponse, 1)
	s.subscribers[ch] = struct{}{}
	initial := cloneUpdateStatus(s.status)
	return ch, initial, func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
	}
}

func (s *updaterServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, initial, unsubscribe := s.subscribeStatus()
	defer unsubscribe()
	if err := writeUpdateEvent(w, initial); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(updaterEventHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case status := <-events:
			if err := writeUpdateEvent(w, status); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeUpdateEvent(w http.ResponseWriter, status updateStatusResponse) error {
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	if status.EventID > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", status.EventID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "event: update\n"); err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}
