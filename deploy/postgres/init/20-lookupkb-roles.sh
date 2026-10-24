#!/bin/sh
# Provision LookupKB's least-privilege database roles. Runs once on first cluster init (docker-entrypoint-initdb.d)
# as the superuser, BEFORE the app ever connects.
#
# Two roles, deliberately separated so the runtime cannot alter the schema:
#   lookupkb_migrator - owns DDL; runs `api migrate`. Non-superuser, but may
#                       CREATE objects in the public schema.
#   lookupkb_app      - the runtime role the API pool connects as. DML only;
#                       the grants themselves are applied by migration 00005.
#
# Role NAMES are a fixed schema contract (migration 00005 grants to them by
# name); only their passwords come from the environment.
set -eu

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-SQL
	DO \$\$
	BEGIN
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'lookupkb_migrator') THEN
			CREATE ROLE lookupkb_migrator LOGIN PASSWORD '${LOOKUPKB_MIGRATOR_DB_PASSWORD}';
		END IF;
		IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'lookupkb_app') THEN
			CREATE ROLE lookupkb_app LOGIN PASSWORD '${LOOKUPKB_APP_DB_PASSWORD}';
		END IF;
	END
	\$\$;

	-- The migrator owns the schema objects it creates; give it create rights.
	GRANT CREATE, USAGE ON SCHEMA public TO lookupkb_migrator;

	-- CREATE on the database lets the non-superuser migrator install the TRUSTED
	-- extensions our first migration needs (citext, pgcrypto) without superuser.
	GRANT CREATE ON DATABASE "${POSTGRES_DB}" TO lookupkb_migrator;

	-- The app role must be able to enter the schema; table/sequence grants are
	-- applied by migration 00005 once the objects exist.
	GRANT USAGE ON SCHEMA public TO lookupkb_app;
SQL
