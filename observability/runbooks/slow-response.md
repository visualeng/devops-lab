# Ответ стал медленным

Алерт `DevopsLabSlowResponse`: p95 задержек держится выше 500 мс.

Метрики скажут «где», но не скажут «почему» — за это отвечают трейсы.

## 1. Найти медленный запрос

В Grafana → Explore → Tempo, запрос по сервису `devops-lab` и Lookback
5 минут. Или напрямую:

```console
$ curl -sG http://localhost:3200/api/search --data-urlencode 'q={.service.name="devops-lab"}' | jq '.traces[0].traceID'
```

По `traceID` открываем сам трейс:

```console
$ curl -s http://localhost:3200/api/traces/<traceID> | jq '.batches[] | .scopeSpans[].spans[] | {name, duration: .durationMs}'
```

## 2. Что искать в дереве span'ов

- серверный span `GET /work` длинный сам по себе — вопрос к самому сервису;
- длинный дочерний `downstream.pricing` — виноват «внешний» вызов, и в
  README про это прямо сказано, что он искусственный;
- длинный span при коротком ответе — смотрим логи по этому `trace_id`.

## 3. Перейти из трейса в логи

В Tempo настроен переход «traces to logs»: из трейса открываются строки
того же запроса в Loki. Вручную это выглядит так:

```logql
{app="devops-lab"} | json | trace_id="<traceID>"
```

## 4. Проверить, не логи ли сами по себе тормозят

```console
$ curl -s http://localhost:3100/loki/api/v1/query_range \
    --data-urlencode 'query=rate({app="devops-lab"}[5m])' \
    --data-urlencode 'start=<now-3600>' --data-urlencode 'end=<now>' \
    --data-urlencode 'step=60' | jq '.data.result'
```

Если поток логов растёт лавинообразно, дело не в сервисе, а в verbosity:
первое, что крутится, — `LOG_LEVEL`.
