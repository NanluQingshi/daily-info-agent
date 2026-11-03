-- Chinese-aware full-text search via zhparser (#55).
--
-- The stock 'simple' config treats CJK text as one long token (no word
-- boundaries), so Chinese queries almost never match. The zhparser
-- extension segments Chinese properly; text search configuration `zh`
-- wires it up with `simple` dictionaries over the resulting tokens
-- (stemming is not useful for Chinese).
--
-- Standalone PostgreSQL: requires superuser for CREATE EXTENSION the
-- first time (or an admin who pre-installed it); docker-compose users
-- get it automatically — see docker/postgres-zhparser/.

-- Idempotent no-op when the extension was already installed by the
-- container initdb script (or a previous run with superuser rights).
CREATE EXTENSION IF NOT EXISTS zhparser;

DROP TEXT SEARCH CONFIGURATION IF EXISTS zh;
CREATE TEXT SEARCH CONFIGURATION zh (parser = zhparser);

-- zhparser emits token types named by single letters; map them all.
ALTER TEXT SEARCH CONFIGURATION zh
    ADD MAPPING FOR a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p,q,r,s,t,u,v,w,x,y,z
    WITH simple;

-- Rebuild the generated tsvector with the Chinese-aware config.
-- PostgreSQL cannot alter a GENERATED column's expression in place, so
-- drop and re-add; dropping the column drops idx_articles_search_tsv.
ALTER TABLE articles DROP COLUMN search_tsv;

ALTER TABLE articles
    ADD COLUMN search_tsv tsvector
        GENERATED ALWAYS AS (
            to_tsvector('zh'::regconfig,
                coalesce(title, '') || ' ' ||
                coalesce(summary, '') || ' ' ||
                left(coalesce(content_text, ''), 100000))
        ) STORED;

CREATE INDEX idx_articles_search_tsv ON articles USING GIN (search_tsv);
