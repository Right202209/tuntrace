package store

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS traffic_aggregated (
    bucket_minute INTEGER NOT NULL,
    process_name  TEXT    NOT NULL DEFAULT '',
    process_path  TEXT    NOT NULL DEFAULT '',
    host          TEXT    NOT NULL DEFAULT '',
    dest_ip       TEXT    NOT NULL DEFAULT '',
    outbound      TEXT    NOT NULL DEFAULT '',
    chains        TEXT    NOT NULL DEFAULT '[]',
    network       TEXT    NOT NULL DEFAULT '',
    upload        INTEGER NOT NULL DEFAULT 0,
    download      INTEGER NOT NULL DEFAULT 0,
    conn_count    INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_minute, process_name, process_path, host, dest_ip, outbound, chains, network)
);

CREATE INDEX IF NOT EXISTS idx_agg_min_proc ON traffic_aggregated(bucket_minute, process_name);
CREATE INDEX IF NOT EXISTS idx_agg_min_host ON traffic_aggregated(bucket_minute, host);
CREATE INDEX IF NOT EXISTS idx_agg_min_ip   ON traffic_aggregated(bucket_minute, dest_ip);
CREATE INDEX IF NOT EXISTS idx_agg_min_out  ON traffic_aggregated(bucket_minute, outbound);

CREATE TABLE IF NOT EXISTS app_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
`
