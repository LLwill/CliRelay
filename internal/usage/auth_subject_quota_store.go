package usage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type AuthSubjectQuotaCycle struct {
	SubjectID      string
	AuthIndex      string
	Provider       string
	QuotaKey       string
	CycleStartAt   time.Time
	ResetAt        time.Time
	WindowSeconds  int64
	LastVerifiedAt time.Time
}

func RecordDailyQuotaSnapshotIdentity(authIndex, authSubjectID, provider string, quotas map[string]*float64) error {
	db := getDB()
	if db == nil {
		return nil
	}

	authIndex = strings.TrimSpace(authIndex)
	authSubjectID = strings.TrimSpace(authSubjectID)
	if authIndex == "" || len(quotas) == 0 {
		return nil
	}
	provider = strings.TrimSpace(provider)
	now := time.Now()
	dateKey := localDayKeyAt(now)
	recordedAt := now.UTC().Format(time.RFC3339Nano)

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("usage: quota snapshot begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`
		INSERT INTO auth_file_quota_snapshots (date_key, auth_index, auth_subject_id, provider, quota_key, percent, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(date_key, auth_index, quota_key) DO UPDATE SET
			auth_subject_id = excluded.auth_subject_id,
			provider = excluded.provider,
			percent = excluded.percent,
			recorded_at = excluded.recorded_at
	`)
	if err != nil {
		return fmt.Errorf("usage: quota snapshot prepare: %w", err)
	}
	defer stmt.Close()

	for key, rawPercent := range quotas {
		quotaKey := strings.TrimSpace(key)
		if quotaKey == "" {
			continue
		}
		var value any
		if rawPercent == nil {
			value = nil
		} else {
			percent := *rawPercent
			if percent < 0 {
				percent = 0
			}
			if percent > 100 {
				percent = 100
			}
			value = percent
		}
		if _, err = stmt.Exec(dateKey, authIndex, authSubjectID, provider, quotaKey, value, recordedAt); err != nil {
			return fmt.Errorf("usage: quota snapshot upsert: %w", err)
		}
	}

	retentionCutoff := cutoffDayKey(7)
	if _, err = tx.Exec(`DELETE FROM auth_file_quota_snapshots WHERE date_key < ?`, retentionCutoff); err != nil {
		return fmt.Errorf("usage: quota snapshot prune: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("usage: quota snapshot commit: %w", err)
	}
	return nil
}

func RecordQuotaSnapshotPointsIdentity(authIndex, authSubjectID, provider string, points []QuotaSnapshotPoint) error {
	db := getDB()
	if db == nil {
		return nil
	}

	authIndex = strings.TrimSpace(authIndex)
	authSubjectID = strings.TrimSpace(authSubjectID)
	if authIndex == "" || len(points) == 0 {
		return nil
	}
	provider = strings.TrimSpace(provider)
	now := time.Now()

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("usage: quota snapshot points begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare(`
		INSERT INTO auth_file_quota_snapshot_points
			(recorded_at, auth_index, auth_subject_id, provider, quota_key, quota_label, percent, reset_at, window_seconds)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("usage: quota snapshot points prepare: %w", err)
	}
	defer stmt.Close()

	for _, point := range points {
		quotaKey := strings.TrimSpace(point.QuotaKey)
		if quotaKey == "" {
			continue
		}
		quotaLabel := strings.TrimSpace(point.QuotaLabel)
		if quotaLabel == "" {
			quotaLabel = quotaKey
		}
		recordedAt := point.RecordedAt
		if recordedAt.IsZero() {
			recordedAt = now
		}
		pointProvider := strings.TrimSpace(point.Provider)
		if pointProvider == "" {
			pointProvider = provider
		}
		var value any
		if point.Percent == nil {
			value = nil
		} else {
			percent := *point.Percent
			if percent < 0 {
				percent = 0
			}
			if percent > 100 {
				percent = 100
			}
			value = percent
		}
		var resetValue any
		if point.ResetAt != nil && !point.ResetAt.IsZero() {
			resetValue = point.ResetAt.UTC().Format(time.RFC3339Nano)
		}
		if _, err = stmt.Exec(
			recordedAt.UTC().Format(time.RFC3339Nano),
			authIndex,
			authSubjectID,
			pointProvider,
			quotaKey,
			quotaLabel,
			value,
			resetValue,
			point.WindowSeconds,
		); err != nil {
			return fmt.Errorf("usage: quota snapshot points insert: %w", err)
		}
		if err = upsertAuthSubjectQuotaCycleTx(tx, authSubjectID, authIndex, pointProvider, quotaKey, point.ResetAt, point.WindowSeconds, recordedAt); err != nil {
			return err
		}
	}

	retentionCutoff := now.AddDate(0, 0, -8).UTC().Format(time.RFC3339Nano)
	if _, err = tx.Exec(`DELETE FROM auth_file_quota_snapshot_points WHERE recorded_at < ?`, retentionCutoff); err != nil {
		return fmt.Errorf("usage: quota snapshot points prune: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("usage: quota snapshot points commit: %w", err)
	}
	return nil
}

func QueryQuotaSnapshotSeriesByAuthSubject(matcher AuthSubjectMatcher, start, end time.Time) ([]QuotaSnapshotSeries, error) {
	points, err := QueryQuotaSnapshotPointsByAuthSubject(matcher, start, end)
	if err != nil {
		return nil, err
	}
	series := make([]QuotaSnapshotSeries, 0)
	indexByKey := make(map[string]int)
	for _, point := range points {
		seriesKey := fmt.Sprintf("%s\x00%d", point.QuotaKey, point.WindowSeconds)
		idx, ok := indexByKey[seriesKey]
		if !ok {
			idx = len(series)
			series = append(series, QuotaSnapshotSeries{
				QuotaKey:      point.QuotaKey,
				QuotaLabel:    point.QuotaLabel,
				WindowSeconds: point.WindowSeconds,
				Points:        []QuotaSnapshotSeriesPoint{},
			})
			indexByKey[seriesKey] = idx
		}
		series[idx].Points = append(series[idx].Points, QuotaSnapshotSeriesPoint{
			Timestamp: point.RecordedAt,
			Percent:   point.Percent,
			ResetAt:   point.ResetAt,
		})
	}
	return series, nil
}

func QueryQuotaSnapshotPointsByAuthSubject(matcher AuthSubjectMatcher, start, end time.Time) ([]QuotaSnapshotPoint, error) {
	db := getDB()
	if db == nil {
		return []QuotaSnapshotPoint{}, nil
	}
	if start.IsZero() {
		start = time.Now().AddDate(0, 0, -7)
	}
	if end.IsZero() {
		end = time.Now()
	}

	matchSQL, matchArgs := buildAuthSubjectQuotaMatchClause(matcher)
	if matchSQL == "" {
		return []QuotaSnapshotPoint{}, nil
	}

	args := make([]interface{}, 0, len(matchArgs)+2)
	args = append(args, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	args = append(args, matchArgs...)

	rows, err := db.Query(fmt.Sprintf(`
		SELECT recorded_at, auth_index, provider, quota_key, quota_label, percent, reset_at, window_seconds
		FROM auth_file_quota_snapshot_points
		WHERE recorded_at >= ? AND recorded_at <= ? AND (%s)
		ORDER BY recorded_at ASC, quota_key ASC
	`, matchSQL), args...)
	if err != nil {
		return nil, fmt.Errorf("usage: quota snapshot points by auth subject query: %w", err)
	}
	defer rows.Close()

	result := make([]QuotaSnapshotPoint, 0)
	for rows.Next() {
		var point QuotaSnapshotPoint
		var recordedAt string
		var resetAt sql.NullString
		var percent sql.NullFloat64
		if err := rows.Scan(
			&recordedAt,
			&point.AuthIndex,
			&point.Provider,
			&point.QuotaKey,
			&point.QuotaLabel,
			&percent,
			&resetAt,
			&point.WindowSeconds,
		); err != nil {
			return nil, fmt.Errorf("usage: quota snapshot points by auth subject scan: %w", err)
		}
		if parsed, ok := parseStoredTime(recordedAt); ok {
			point.RecordedAt = parsed
		}
		if percent.Valid {
			value := percent.Float64
			point.Percent = &value
		}
		if resetAt.Valid {
			if parsed, ok := parseStoredTime(resetAt.String); ok {
				point.ResetAt = &parsed
			}
		}
		result = append(result, point)
	}
	return result, rows.Err()
}

func QueryLatestWeeklyQuotaCycleByAuthSubject(subjectID string, quotaKeys ...string) (*AuthSubjectQuotaCycle, error) {
	db := getDB()
	if db == nil {
		return nil, nil
	}
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil, nil
	}
	normalizedKeys := dedupeExactStrings(quotaKeys)

	var cycle AuthSubjectQuotaCycle
	var cycleStartRaw string
	var resetRaw string
	var verifiedRaw string
	query := `
		SELECT subject_id, auth_index, provider, quota_key, cycle_start_at, reset_at, window_seconds, last_verified_at
		FROM auth_subject_quota_cycles
		WHERE subject_id = ? AND window_seconds >= 604800
	`
	args := make([]interface{}, 0, 1+len(normalizedKeys))
	args = append(args, subjectID)
	if len(normalizedKeys) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(normalizedKeys)), ",")
		query += " AND quota_key IN (" + placeholders + ")"
		for _, quotaKey := range normalizedKeys {
			args = append(args, quotaKey)
		}
	}
	query += `
		ORDER BY last_verified_at DESC, reset_at DESC
		LIMIT 1
	`
	err := db.QueryRow(query, args...).Scan(
		&cycle.SubjectID,
		&cycle.AuthIndex,
		&cycle.Provider,
		&cycle.QuotaKey,
		&cycleStartRaw,
		&resetRaw,
		&cycle.WindowSeconds,
		&verifiedRaw,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("usage: auth subject quota cycle query: %w", err)
	}
	if parsed, ok := parseStoredTime(cycleStartRaw); ok {
		cycle.CycleStartAt = parsed
	}
	if parsed, ok := parseStoredTime(resetRaw); ok {
		cycle.ResetAt = parsed
	}
	if parsed, ok := parseStoredTime(verifiedRaw); ok {
		cycle.LastVerifiedAt = parsed
	}
	if cycle.CycleStartAt.IsZero() || cycle.ResetAt.IsZero() || cycle.WindowSeconds <= 0 {
		return nil, nil
	}
	return &cycle, nil
}

func buildAuthSubjectQuotaMatchClause(matcher AuthSubjectMatcher) (string, []interface{}) {
	subjectID := strings.TrimSpace(matcher.SubjectID)
	authIndexes := dedupeExactStrings(matcher.AuthIndexes)
	clauses := make([]string, 0, 2)
	args := make([]interface{}, 0, 1+len(authIndexes))

	if subjectID != "" {
		clauses = append(clauses, "trim(coalesce(auth_subject_id, '')) = ?")
		args = append(args, subjectID)
	}
	if len(authIndexes) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(authIndexes)), ",")
		clauses = append(clauses, "(trim(coalesce(auth_subject_id, '')) = '' AND auth_index IN ("+placeholders+"))")
		for _, value := range authIndexes {
			args = append(args, value)
		}
	}
	return strings.Join(clauses, " OR "), args
}

func upsertAuthSubjectQuotaCycleTx(tx *sql.Tx, authSubjectID, authIndex, provider, quotaKey string, resetAt *time.Time, windowSeconds int64, recordedAt time.Time) error {
	authSubjectID = strings.TrimSpace(authSubjectID)
	quotaKey = strings.TrimSpace(quotaKey)
	if authSubjectID == "" || quotaKey == "" || resetAt == nil || resetAt.IsZero() || windowSeconds <= 0 {
		return nil
	}

	cycleStart := resetAt.UTC().Add(-time.Duration(windowSeconds) * time.Second)
	recordedAt = recordedAt.UTC()
	_, err := tx.Exec(`
		INSERT INTO auth_subject_quota_cycles
			(subject_id, auth_index, provider, quota_key, cycle_start_at, reset_at, window_seconds, last_verified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(subject_id, quota_key) DO UPDATE SET
			auth_index = excluded.auth_index,
			provider = excluded.provider,
			cycle_start_at = excluded.cycle_start_at,
			reset_at = excluded.reset_at,
			window_seconds = excluded.window_seconds,
			last_verified_at = excluded.last_verified_at
	`,
		authSubjectID,
		strings.TrimSpace(authIndex),
		strings.TrimSpace(provider),
		quotaKey,
		cycleStart.Format(time.RFC3339Nano),
		resetAt.UTC().Format(time.RFC3339Nano),
		windowSeconds,
		recordedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("usage: auth subject quota cycle upsert: %w", err)
	}
	return nil
}
