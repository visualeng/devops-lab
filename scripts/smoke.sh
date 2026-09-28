#!/usr/bin/env bash
# Дымовая проверка сервиса: /healthz отвечает, /info совпадает с ожидаемым
# коммитом, корень живой, неизвестный путь даёт 404.
#
# Используется и в CI после поднятия стенда, и вручную после деплоя:
#   BASE_URL=http://localhost:8080 EXPECTED_COMMIT=abc1234 ./scripts/smoke.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
EXPECTED_COMMIT="${EXPECTED_COMMIT:-}"

fail() {
	echo "smoke: $*" >&2
	exit 1
}

health="$(curl -fsS --max-time 5 "${BASE_URL}/healthz")" || fail "не дождались /healthz на ${BASE_URL}"
echo "${health}" | grep -q '"status":"ok"' || fail "неожиданный /healthz: ${health}"

info="$(curl -fsS --max-time 5 "${BASE_URL}/info")" || fail "не дождались /info на ${BASE_URL}"
echo "smoke: /info ${info}"

if [ -n "${EXPECTED_COMMIT}" ]; then
	echo "${info}" | grep -q "\"commit\":\"${EXPECTED_COMMIT}\"" \
		|| fail "на сервере другой коммит, ждали ${EXPECTED_COMMIT}"
fi

code="$(curl -s -o /dev/null -w '%{http_code}' "${BASE_URL}/")"
[ "${code}" = "200" ] || fail "корень вернул ${code}"

code="$(curl -s -o /dev/null -w '%{http_code}' "${BASE_URL}/nope")"
[ "${code}" = "404" ] || fail "неизвестный путь вернул ${code} вместо 404"

# Метрики тоже часть сервиса: если /metrics исчез, Prometheus молча
# перестанет видеть стенд.
metrics="$(curl -fsS --max-time 5 "${BASE_URL}/metrics")" || fail "не дождались /metrics"
echo "${metrics}" | grep -q 'devops_lab_build_info' \
	|| fail "в /metrics нет devops_lab_build_info"

echo "smoke: ок"
