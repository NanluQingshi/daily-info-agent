CREATE TABLE run_logs (
    id              BIGSERIAL PRIMARY KEY,
    run_id          TEXT        NOT NULL UNIQUE,
    total_fetched   INT         NOT NULL DEFAULT 0,
    total_processed INT         NOT NULL DEFAULT 0,
    total_saved     INT         NOT NULL DEFAULT 0,
    total_published INT         NOT NULL DEFAULT 0,
    total_skipped   INT         NOT NULL DEFAULT 0,
    total_failed    INT         NOT NULL DEFAULT 0,
    duration_ms     BIGINT      NOT NULL DEFAULT 0,
    fatal_error     TEXT        NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_run_logs_started_at ON run_logs (started_at DESC);
