#!/usr/bin/env bash
set -euo pipefail

CONTAINER="${1:-knowledge-core_postgres_1}"
BACKUP_DIR="${2:-}"

if [ -z "${BACKUP_DIR}" ]; then
    echo "Usage:"
    echo "  $0 <container> <backup-directory>"
    echo
    echo "Example:"
    echo "  $0 postgres ./postgres-backups/20260922_160500"
    exit 1
fi

DATABASE_DUMP="${BACKUP_DIR}/database.dump"
GLOBALS_DUMP="${BACKUP_DIR}/globals.sql"

if [ ! -f "${DATABASE_DUMP}" ]; then
    echo "ERROR: Missing ${DATABASE_DUMP}" >&2
    exit 1
fi

if [ ! -f "${GLOBALS_DUMP}" ]; then
    echo "ERROR: Missing ${GLOBALS_DUMP}" >&2
    exit 1
fi

if ! podman container exists "${CONTAINER}"; then
    echo "ERROR: Container '${CONTAINER}' does not exist." >&2
    exit 1
fi

if [ "$(podman inspect -f '{{.State.Running}}' "${CONTAINER}")" != "true" ]; then
    echo "ERROR: Container '${CONTAINER}' is not running." >&2
    exit 1
fi

POSTGRES_DB="$(
    podman exec "${CONTAINER}" sh -c 'printf "%s" "$POSTGRES_DB"'
)"

POSTGRES_USER="$(
    podman exec "${CONTAINER}" sh -c 'printf "%s" "$POSTGRES_USER"'
)"

if [ -z "${POSTGRES_DB}" ]; then
    echo "ERROR: POSTGRES_DB is empty." >&2
    exit 1
fi

if [ -z "${POSTGRES_USER}" ]; then
    echo "ERROR: POSTGRES_USER is empty." >&2
    exit 1
fi

echo "PostgreSQL restore"
echo "  container: ${CONTAINER}"
echo "  database:  ${POSTGRES_DB}"
echo "  user:      ${POSTGRES_USER}"
echo "  source:    ${BACKUP_DIR}"
echo

echo "Verifying backup..."

podman exec -i "${CONTAINER}" pg_restore --list <"${DATABASE_DUMP}"    >/dev/null

echo "Backup is readable."
echo

#
# Globální role
#
# Používáme --clean při pg_dumpall nemáme, proto při opakovaném
# restore mohou některé CREATE ROLE příkazy skončit chybou.
# Pro běžný disaster recovery do čerstvého PostgreSQL je to v pořádku.
#
echo "Restoring PostgreSQL globals..."

podman exec -i "${CONTAINER}" sh -c '
        export PGPASSWORD="$POSTGRES_PASSWORD"

        exec psql \
            --username="$POSTGRES_USER" \
            --dbname=postgres \
            --set=ON_ERROR_STOP=0
' < "${GLOBALS_DUMP}"

echo

echo "Dropping existing database ${POSTGRES_DB}..."

podman exec "${CONTAINER}" sh -c '
    export PGPASSWORD="$POSTGRES_PASSWORD"

    dropdb \
        --username="$POSTGRES_USER" \
        --if-exists \
        --force \
        "$POSTGRES_DB"

    createdb \
        --username="$POSTGRES_USER" \
        --owner="$POSTGRES_USER" \
        "$POSTGRES_DB"
'


echo
echo "Restoring database..."

podman exec -i "${CONTAINER}" sh -c '
        export PGPASSWORD="$POSTGRES_PASSWORD"

        exec pg_restore \
            --username="$POSTGRES_USER" \
            --dbname="$POSTGRES_DB" \
            --verbose \
            --exit-on-error
' < "${DATABASE_DUMP}"

echo
echo "Running ANALYZE..."

podman exec "${CONTAINER}" sh -c '
    export PGPASSWORD="$POSTGRES_PASSWORD"

    exec vacuumdb \
        --username="$POSTGRES_USER" \
        --dbname="$POSTGRES_DB" \
        --analyze-only
'

echo
echo "Restore completed successfully."
