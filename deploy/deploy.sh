#!/usr/bin/env bash
# Деплой на сервер: тянет образ по digest, поднимает контейнер и ждёт
# healthcheck. Скрипт выполняется на самой машине — из CI приходит по ssh.
#
#   deploy/deploy.sh ghcr.io/visualeng/devops-lab@sha256:...
#
# Тег намеренно не принимается: задеплоить нужно ровно то, что проверено.
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/devops-lab}"
IMAGE_REF="${1:?нужен образ в виде repo@sha256:...}"
STATE_DIR="${APP_DIR}/state"
CURRENT_FILE="${STATE_DIR}/current"
HISTORY_FILE="${STATE_DIR}/history"

REPO="${IMAGE_REF%@*}"
DIGEST="${IMAGE_REF##*@}"

case "${DIGEST}" in
sha256:*) ;;
*)
	echo "deploy: нужен digest, а не тег: ${IMAGE_REF}" >&2
	exit 1
	;;
esac

cd "${APP_DIR}"
previous="$(cat "${CURRENT_FILE}" 2>/dev/null || true)"

echo "deploy: тяну ${IMAGE_REF}"
IMAGE="${REPO}" DIGEST="${DIGEST}" docker compose -f docker/compose.prod.yaml pull --quiet
IMAGE="${REPO}" DIGEST="${DIGEST}" docker compose -f docker/compose.prod.yaml \
	up --detach --wait --wait-timeout 60

mkdir -p "${STATE_DIR}"
if [ -n "${previous}" ] && [ "${previous}" != "${DIGEST}" ]; then
	printf '%s\n' "${previous}" >>"${HISTORY_FILE}"
	echo "deploy: предыдущий ${previous} (откат — deploy/rollback.sh)"
fi
printf '%s' "${DIGEST}" >"${CURRENT_FILE}"

echo "deploy: готово, работает ${IMAGE_REF}"
