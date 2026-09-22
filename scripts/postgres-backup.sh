#!/usr/bin/env bash
set -euo pipefail

CONTAINER="${1:-knowledge-core_postgres_1}"
BACKUP_ROOT="${2:-./postgres-backups}"

TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_DIR="${BACKUP_ROOT}/${TIMESTAMP}"

mkdir -p "${BACKUP_DIR}"

echo "PostgreSQL backup"
echo "  container: ${CONTAINER}"
echo "  target:    ${BACKUP_DIR}"

# Ověření, že kontejner běží
if ! podman container exists "${CONTAINER}"; then
    echo "ERROR: Container '${CONTAINER}' does not exist." >&2
    exit 1
fi

if [ "$(podman inspect -f '{{.State.Running}}' "${CONTAINER}")" != "true" ]; then
    echo "ERROR: Container '${CONTAINER}' is not running." >&2
    exit 1
fi

# Informace o databázi
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

echo "  database:  ${POSTGRES_DB}"
echo "  user:      ${POSTGRES_USER}"

echo
echo "Backing up database..."

podman exec "${CONTAINER}" sh -c '
    export PGPASSWORD="$POSTGRES_PASSWORD"

    exec pg_dump \
        --username="$POSTGRES_USER" \
        --dbname="$POSTGRES_DB" \
        --format=custom \
        --verbose
' > "${BACKUP_DIR}/database.dump"

echo
echo "Backing up PostgreSQL globals..."

podman exec "${CONTAINER}" sh -c '
    export PGPASSWORD="$POSTGRES_PASSWORD"

    exec pg_dumpall \
        --username="$POSTGRES_USER" \
        --globals-only
' > "${BACKUP_DIR}/globals.sql"

# Metadata
cat > "${BACKUP_DIR}/backup.info" <<EOF
timestamp=${TIMESTAMP}
container=${CONTAINER}
database=${POSTGRES_DB}
user=${POSTGRES_USER}
EOF

echo
echo "Verifying dump..."

cat "${BACKUP_DIR}/database.dump" |
    podman exec -i "${CONTAINER}" pg_restore --list >/dev/null

if [ ! -s "${BACKUP_DIR}/database.dump" ]; then
    echo "ERROR: Database dump is empty." >&2
    exit 1
fi

if [ ! -s "${BACKUP_DIR}/globals.sql" ]; then
    echo "ERROR: Globals dump is empty." >&2
    exit 1
fi

echo
echo "Backup completed successfully:"
du -h "${BACKUP_DIR}/database.dump" "${BACKUP_DIR}/globals.sql"

echo
echo "Backup directory:"
echo "${BACKUP_DIR}"
