# Доля ответов 5xx выше порога

Алерт `DevopsLabHighErrorRate`: больше 5% запросов за 10 минут возвращают 5xx.

## 1. Подтвердить, что это не деплой

```console
$ curl -s http://<host>/info            # какой коммит сейчас живёт
$ kubectl get pods -l app.kubernetes.io/name=devops-lab -o wide
$ kubectl rollout history deployment/devops-lab
```

Если алерт совпал с раскаткой — скорее всего, откатываемся (см.
`service-down.md`) и разбираем причину уже на конкретной версии.

## 2. Понять, какие маршруты падают

В Prometheus:

```promql
sum by (handler, code) (rate(devops_lab_http_requests_total[5m]))
```

Покажет, где именно 5xx: один маршрут или всё сразу.

## 3. Проверить, не кончился ли ресурс

```console
$ kubectl top pods -l app.kubernetes.io/name=devops-lab
$ docker stats --no-stream
```

Частая история: память упёрлась в лимит, рантайм отдаёт 5xx на каждый
запрос, и это видно по `go_goroutines` и `process_resident_memory_bytes`.

## 4. Если сервис жив, а 5xx идут

Смотрим код ответа по маршруту: если это 500 из-за зависимости (база,
внешний API) — проблема не в стенде, а там, куда он ходит.
