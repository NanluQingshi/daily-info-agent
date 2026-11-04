-- Track items the AI never returned a result for, separately from items it
-- actually enriched. Without this column "processed" counts LLM outages as
-- successful work, which is the same blind spot as a zero-fetch run.
ALTER TABLE run_logs
    ADD COLUMN IF NOT EXISTS total_llm_skipped integer NOT NULL DEFAULT 0;
