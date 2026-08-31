# PostgreSQL 16 with the zhparser full-text-search extension (#55).
#
# Base: official postgres:16 (Debian). Its server is built from the PGDG
# apt packages, so `postgresql-server-dev-16` headers match the running
# server exactly — required for a clean extension build.
#
# Build layer (scws + zhparser) is purged afterwards to keep the runtime
# image lean; only the extension artifacts and libscws remain.

FROM postgres:16

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        git \
        postgresql-server-dev-16 \
    && rm -rf /var/lib/apt/lists/*

# SCWS — Simple Chinese Word Segmentation, zhparser's tokenizer engine.
RUN git clone --depth 1 https://github.com/hightman/scws.git /tmp/scws \
    && cd /tmp/scws \
    && ./configure \
    && make install \
    && ldconfig \
    && rm -rf /tmp/scws

# zhparser — PostgreSQL text-search parser on top of SCWS.
RUN git clone --depth 1 https://github.com/amosbird/zhparser.git /tmp/zhparser \
    && cd /tmp/zhparser \
    && make \
    && make install \
    && rm -rf /tmp/zhparser \
    && apt-get purge -y build-essential git postgresql-server-dev-16 \
    && apt-get autoremove -y \
    && rm -rf /var/lib/apt/lists/*
