#!/bin/sh
# Provision the Authentik database and role alongside the LookupKB app DB.
# Runs once on first cluster init (docker-entrypoint-initdb.d). Values come
# from the postgres service environment.
#
# NOTE (docs/planning/06-security.md §8): least-privilege per-service DB roles
# and a separate migration role are a later hardening ticket; this skeleton
# creates functional roles only.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
	DO \$\$
	BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${AUTHENTIK_DB_USER}') THEN
			CREATE ROLE ${AUTHENTIK_DB_USER} LOGIN PASSWORD '${AUTHENTIK_DB_PASSWORD}';
		END IF;
	END
	\$\$;
SQL

# CREATE DATABASE cannot run inside the DO block / a transaction; guard it.
if ! psql -tAc "SELECT 1 FROM pg_database WHERE datname = '${AUTHENTIK_DB}'" \
	--username "$POSTGRES_USER" --dbname "$POSTGRES_DB" | grep -q 1; then
	createdb --username "$POSTGRES_USER" --owner "${AUTHENTIK_DB_USER}" "${AUTHENTIK_DB}"
fi
