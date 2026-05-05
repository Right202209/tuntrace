package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

type AggregateRow struct {
	BucketMinute int64
	ProcessName  string
	ProcessPath  string
	Host         string
	DestIP       string
	Outbound     string
	Chains       string
	Network      string
	Upload       int64
	Download     int64
	ConnCount    int64
}

const upsertAggregateSQL = `
INSERT INTO traffic_aggregated (
    bucket_minute, process_name, process_path, host, dest_ip,
    outbound, chains, network, upload, download, conn_count
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (bucket_minute, process_name, process_path, host, dest_ip, outbound, chains, network)
DO UPDATE SET upload     = upload     + excluded.upload,
              download   = download   + excluded.download,
              conn_count = conn_count + excluded.conn_count
`

func (s *Store) InsertAggregateBatch(ctx context.Context, rows []AggregateRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, upsertAggregateSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.ExecContext(ctx,
			r.BucketMinute, r.ProcessName, r.ProcessPath, r.Host, r.DestIP,
			r.Outbound, r.Chains, r.Network, r.Upload, r.Download, r.ConnCount,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type SummaryRow struct {
	Label    string `json:"label"`
	Upload   int64  `json:"upload"`
	Download int64  `json:"download"`
	Count    int64  `json:"count"`
}

func (s *Store) QuerySummary(ctx context.Context, dim string, fromMin, toMin int64, top int) ([]SummaryRow, error) {
	col, err := dimColumn(dim)
	if err != nil {
		return nil, err
	}
	if top <= 0 {
		top = 20
	}
	q := fmt.Sprintf(`
        SELECT %s AS label, SUM(upload), SUM(download), SUM(conn_count)
        FROM traffic_aggregated
        WHERE bucket_minute BETWEEN ? AND ?
        GROUP BY %s
        ORDER BY (SUM(upload) + SUM(download)) DESC
        LIMIT ?
    `, col, col)
	rows, err := s.db.QueryContext(ctx, q, fromMin, toMin, top)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SummaryRow{}
	for rows.Next() {
		var r SummaryRow
		if err := rows.Scan(&r.Label, &r.Upload, &r.Download, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type TimeseriesPoint struct {
	Minute   int64 `json:"minute"`
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
}

func (s *Store) QueryTimeseries(ctx context.Context, dim, value string, fromMin, toMin int64) ([]TimeseriesPoint, error) {
	col, err := dimColumn(dim)
	if err != nil {
		return nil, err
	}
	q := fmt.Sprintf(`
        SELECT bucket_minute, SUM(upload), SUM(download)
        FROM traffic_aggregated
        WHERE bucket_minute BETWEEN ? AND ? AND %s = ?
        GROUP BY bucket_minute
        ORDER BY bucket_minute ASC
    `, col)
	rows, err := s.db.QueryContext(ctx, q, fromMin, toMin, value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimeseriesPoint{}
	for rows.Next() {
		var p TimeseriesPoint
		if err := rows.Scan(&p.Minute, &p.Upload, &p.Download); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func dimColumn(dim string) (string, error) {
	switch strings.ToLower(dim) {
	case "process":
		return "process_name", nil
	case "host":
		return "host", nil
	case "ip":
		return "dest_ip", nil
	case "outbound":
		return "outbound", nil
	default:
		return "", fmt.Errorf("invalid dim %q", dim)
	}
}

// MissingProcessRatio returns bytes-weighted fraction of rows in [fromMin,toMin] with empty process_name.
// The UI uses this to show a "find-process-mode not enabled" banner.
func (s *Store) MissingProcessRatio(ctx context.Context, fromMin, toMin int64) (float64, error) {
	var total, missing sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
        SELECT
            COALESCE(SUM(upload + download), 0),
            COALESCE(SUM(CASE WHEN process_name = '' THEN upload + download ELSE 0 END), 0)
        FROM traffic_aggregated
        WHERE bucket_minute BETWEEN ? AND ?
    `, fromMin, toMin).Scan(&total, &missing)
	if err != nil {
		return 0, err
	}
	if total.Int64 == 0 {
		return 0, nil
	}
	return float64(missing.Int64) / float64(total.Int64), nil
}

// Prune deletes rows older than retentionDays. Returns rows affected.
func (s *Store) Prune(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix() / 60
	res, err := s.db.ExecContext(ctx, `DELETE FROM traffic_aggregated WHERE bucket_minute < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) Vacuum(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}

func (s *Store) SettingGet(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

func (s *Store) SettingSet(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO app_settings (key, value) VALUES (?, ?)
        ON CONFLICT(key) DO UPDATE SET value = excluded.value
    `, key, value)
	return err
}
