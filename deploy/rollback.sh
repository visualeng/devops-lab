#!/usr/bin/env bash
# Откат на предыдущий образ: берёт последний digest из истории и зовёт
# тот же deploy.sh. Второй откат вернёт ещё на один шаг назад.
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/devops-lab}"
IMAGE_REPO="${IMAGE_REPO:?укажите репозиторий образа, например ghcr.io/visualeng/devops-lab}"
HISTORY_FILE="${APP_DIR}/state/history"

last="$(tail -n 1 "${HISTORY_FILE}" 2>/dev/null || true)"
if [ -z "${last}" ]; then
	echo "rollback: истории нет, откатываться не на что" >&2
	exit 1
fi

# запись уходит из истории, иначе повторный откат вернул бы тот же образ
sed -i '$d' "${HISTORY_FILE}"

echo "rollback: возвращаю ${IMAGE_REPO}@${last}"
exec "$(dirname "$0")/deploy.sh" "${IMAGE_REPO}@${last}"
