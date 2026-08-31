-- Track per-run full-text extraction counts alongside the other stage counts.
ALTER TABLE run_logs
    ADD COLUMN total_extracted INT NOT NULL DEFAULT 0;
