package usage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type QuotaSnapshotPoint struct {
	RecordedAt    time.Time  `json:"recorded_at"`
	AuthIndex     string     `json:"auth_index"`
	Provider      string     `json:"provider"`
	QuotaKey      string     `json:"quota_key"`
	QuotaLabel    string     `json:"quota_label"`
	Percent       *float64   `json:"percent"`
	ResetAt       *time.Time `json:"reset_at,omitempty"`
	WindowSeconds int64      `json:"window_seconds"`
}

type QuotaSnapshotSeriesPoint struct {
	Timestamp time.Time  `json:"timestamp"`
	Percent   *float64   `json:"percent"`
	ResetAt   *time.Time `json:"reset_at,omitempty"`
}

type QuotaSnapshotSeries struct {
	QuotaKey      string                     `json:"quota_key"`
	QuotaLabel    string                     `json:"quota_label"`
	WindowSeconds int64                      `json:"window_seconds"`
	Points        []QuotaSnapshotSeriesPoint `json:"points"`
}

func RecordDailyQuotaSnapshot(authIndex, provider string, quotas map[string]*float64) error {
	return RecordDailyQuotaSnapshotIdentity(authIndex, "", provider, quotas)
}

func RecordQuotaSnapshotPoints(authIndex, provider string, points []QuotaSnapshotPoint) error {
	return RecordQuotaSnapshotPointsIdentity(authIndex, "", provider, points)
}

func QueryDailyQuotaByAuthIndexes(authIndexes []string, quotaKey string, days int) ([]DailyQuotaPoint, error) {
	db := getDB()
	if db == nil {
		return []DailyQuotaPoint{}, nil
	}
	if days < 1 {
		days = 7
	}
	if len(authIndexes) == 0 {
		return []DailyQuotaPoint{}, nil
	}
	quotaKey = strings.TrimSpace(quotaKey)
	if quotaKey == "" {
		return []DailyQuotaPoint{}, nil
	}

	seen := make(map[string]struct{}, len(authIndexes))
	normalized := make([]string, 0, len(authIndexes))
	for _, idx := range authIndexes {
		idx = strings.TrimSpace(idx)
		if idx == "" {
			continue
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		normalized = append(normalized, idx)
	}
	if len(normalized) == 0 {
		return []DailyQuotaPoint{}, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(normalized)), ",")
	args := make([]interface{}, 0, len(normalized)+2)
	args = append(args, cutoffDayKey(days), quotaKey)
	for _, idx := range normalized {
		args = append(args, idx)
	}

	q := fmt.Sprintf(`
		SELECT date_key, AVG(percent) AS avg_percent, COUNT(percent) AS samples
		FROM auth_file_quota_snapshots
		WHERE date_key >= ? AND quota_key = ? AND auth_index IN (%s) AND percent IS NOT NULL
		GROUP BY date_key
		ORDER BY date_key ASC
	`, placeholders)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("usage: daily quota by auth indexes query: %w", err)
	}
	defer rows.Close()

	result := make([]DailyQuotaPoint, 0, days)
	for rows.Next() {
		var point DailyQuotaPoint
		var percent sql.NullFloat64
		if err := rows.Scan(&point.Date, &percent, &point.Samples); err != nil {
			return nil, fmt.Errorf("usage: daily quota by auth indexes scan: %w", err)
		}
		if percent.Valid {
			v := percent.Float64
			point.Percent = &v
		}
		result = append(result, point)
	}
	return result, rows.Err()
}

func QueryQuotaSnapshotPoints(authIndex string, start, end time.Time) ([]QuotaSnapshotPoint, error) {
	db := getDB()
	if db == nil {
		return []QuotaSnapshotPoint{}, nil
	}
	authIndex = strings.TrimSpace(authIndex)
	if authIndex == "" {
		return []QuotaSnapshotPoint{}, nil
	}
	if start.IsZero() {
		start = time.Now().AddDate(0, 0, -7)
	}
	if end.IsZero() {
		end = time.Now()
	}

	rows, err := db.Query(`
		SELECT recorded_at, auth_index, provider, quota_key, quota_label, percent, reset_at, window_seconds
		FROM auth_file_quota_snapshot_points
		WHERE auth_index = ? AND recorded_at >= ? AND recorded_at <= ?
		ORDER BY recorded_at ASC, quota_key ASC
	`, authIndex, start.UTC().Format(time.RFC3339Nano), end.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("usage: quota snapshot points query: %w", err)
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
			return nil, fmt.Errorf("usage: quota snapshot points scan: %w", err)
		}
		if parsed, ok := parseStoredTime(recordedAt); ok {
			point.RecordedAt = parsed
		}
		if percent.Valid {
			v := percent.Float64
			point.Percent = &v
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

func QueryQuotaSnapshotSeries(authIndex string, start, end time.Time) ([]QuotaSnapshotSeries, error) {
	points, err := QueryQuotaSnapshotPoints(authIndex, start, end)
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
			indexByKey[seriesKey] = idx
			series = append(series, QuotaSnapshotSeries{
				QuotaKey:      point.QuotaKey,
				QuotaLabel:    point.QuotaLabel,
				WindowSeconds: point.WindowSeconds,
				Points:        []QuotaSnapshotSeriesPoint{},
			})
		}
		series[idx].Points = append(series[idx].Points, QuotaSnapshotSeriesPoint{
			Timestamp: point.RecordedAt,
			Percent:   point.Percent,
			ResetAt:   point.ResetAt,
		})
	}
	return series, nil
}
