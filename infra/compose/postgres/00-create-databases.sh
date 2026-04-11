#!/bin/bash
# =============================================================================
# Postgres bootstrap — create secondary databases before init.sql runs.
#
# docker-entrypoint-initdb.d executes files in alphabetical order, so the
# leading `00-` prefix ensures this runs before `01-init.sql`. We create
# the personel_keycloak database here (Keycloak refuses to share a DB
# with the application schema) and give the dev postgres user permission
# on it.
#
# This file is intentionally shell-based because CREATE DATABASE cannot
# run inside a transaction, and psql's -f mode wraps each file in one
# implicit transaction. Invoking psql multiple times with -c is the
# cleanest workaround.
# =============================================================================
set -euo pipefail

psql -v ON_ERROR_STOP=1 \
    --username "$POSTGRES_USER" \
    --dbname "$POSTGRES_DB" <<-EOSQL
    SELECT 'CREATE DATABASE personel_keycloak'
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'personel_keycloak')\gexec
EOSQL
