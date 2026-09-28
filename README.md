# devops-lab

[![ci](https://github.com/visualeng/devops-lab/actions/workflows/ci.yml/badge.svg)](https://github.com/visualeng/devops-lab/actions/workflows/ci.yml)

Учебный стенд, на котором один маленький сервис проходит весь путь до
прода: коммит → тесты → образ в реестре → сканирование и подпись →
стенд с дымовой проверкой → ручной деплой на сервер с откатом.

Сам сервис здесь намеренно простой (три ручки: `/`, `/info`, `/healthz`):
весь смысл репозитория в том, что происходит **вокруг** него. Всё, что
здесь показано, реально выполняется в GitHub Actions — это видно по
статусам и логам прогонов, а не по описанию в тексте.

## Путь изменения

```
push / pull request
        │
        ▼
   ┌─────────┐   gofmt, vet, тест с -race, govulncheck
   │  test   │   linux и windows параллельно
   └────┬────┘
        │
        ▼
   ┌─────────┐   сборка в две стадии, кэш в GH Actions,
   │  image  │   теги по коммиту и main, SBOM и provenance
   └──┬───┬──┘   от buildx в attestation
      │   │
      │   ├──────────────► ┌───────────┐  Trivy (CRITICAL/HIGH валят
      │                   │ security  │  сборку), SBOM CycloneDX
      │                   │           │  в артефакты, cosign keyless
      │                   └─────┬─────┘  sign + verify
      │                         │
      └────────────┬────────────┘
                   ▼
             ┌───────────┐   тянет образ из GHCR по digest,
             │  staging  │   поднимает compose, ждёт healthcheck,
             └───────────┘   гоняет scripts/smoke.sh
                   │
                   │  workflow_dispatch + окружение production
                   ▼
             ┌───────────┐   тег → digest на раннере, деплой по ssh,
             │  deploy   │   дымовая проверка, откат на предыдущий
             └───────────┘   образ при неудаче
```

Ключевая деталь: стенд поднимает **не пересобранный локально образ**, а
тот, что уже лежит в реестре, по digest. Проверяется артефакт, который
потом поедет на сервер, а не исходники.

## Что внутри

```
app/                  сервис-подопытный на Go: healthz, info, work, метрики, логи, трейсы
docker/compose.yaml   локальный стенд: собрать и поднять
docker/compose.ci.yaml   стенд в CI: чужой образ по digest, ничего не собирает
docker/compose.prod.yaml  прод: digest вместо тега, read-only, cap_drop, лимит логов
docker/compose.observability.yaml  стек наблюдаемости: Loki, Tempo, Alloy, Grafana
deploy/deploy.sh      деплой на сервере: pull по digest, up --wait, запись состояния
deploy/rollback.sh    откат на предыдущий digest из истории
scripts/smoke.sh      дымовая проверка: /healthz, /info, /work, /metrics, 404 на мусор
infra/terraform/      VPS как код: сервер, статический IP, файрвол
infra/ansible/        роли base/docker/app: провижининг и деплой
k8s/charts/           helm-чарт: деплой, Service, Ingress, HPA, PDB, NetworkPolicy
observability/prometheus/  метрики: recording-правила, алерты, SLO и бюджет ошибок
observability/loki/   конфиг Loki: логи в одном процессе, хранение неделю
observability/tempo/  конфиг Tempo: OTLP и span-метрики в Prometheus
observability/alloy/  агент: читает логи контейнеров и шлёт их в Loki
observability/grafana/  два дашборда (метрики и логи) и переходы трейс → логи
observability/runbooks/  что делать, когда сработал алерт
```

## Сервис

Пять ручек и никакой базы — весь фокус на конвейере:

```console
$ curl localhost:8080/healthz
{"status":"ok"}

$ curl localhost:8080/info
{"builtAt":"2026-09-28T11:40:56Z","commit":"b8936c5c44f9a7b5ef582abab24df91907195bc6","service":"devops-lab","version":"main"}

$ curl localhost:8080/metrics | grep devops_lab_build_info
devops_lab_build_info{commit="b8936c5c44f9a7b5ef582abab24df91907195bc6",version="main"} 1

$ curl localhost:8080/work
{"service":"devops-lab","slept_ms":25}
```

`/info` отдаёт ровно тот коммит, который был в сборке образа, — этим
пользуются и дымовая проверка (сверяет с SHA коммита), и деплой (проверяет,
что на сервере именно то, что просили). Сам сервис умеет проверять своё
здоровье ключом `-healthcheck`: в финальном образе нет ни shell, ни curl,
поэтому healthcheck выполняет бинарник.

`/work` — искусственный «внешний» вызов с задержкой `WORK_DELAY_MS`.
Смысл в нём один: внутри есть дочерний span, поэтому в Tempo видно дерево
трейса, а не один серверный span, и есть на чём показать, где копится время.

Метрики отдаёт сам сервис: счётчик запросов по маршрутам и кодам,
гистограмма задержек и `devops_lab_build_info` с версией и коммитом.
Метка маршрута берётся из шаблона `ServeMux` (`GET /healthz`), а не из
пути запроса — иначе в метки попал бы каждый конкретный id.

Логи пишутся в JSON, по строке на запрос, с полями `method`, `route`,
`status`, `duration_ms` и без версии в каждой записи — она есть всегда:

```console
$ curl -s localhost:8080/info >/dev/null
$ docker compose logs app | tail -1
{"time":"2026-09-28T13:22:19Z","level":"INFO","msg":"request","service":"devops-lab","version":"main","commit":"b8936c5c...","method":"GET","route":"GET /info","status":200,"duration_ms":0.42}
```

Трейсы шлются в Tempo по OTLP, но только если задан адрес коллектора
(`OTEL_EXPORTER_OTLP_ENDPOINT`). Без него сервис работает как ни в чём не
бывало — стенд не должен падать из-за того, что Tempo ещё не поднят.

## Локальный запуск

Нужен только Docker:

```console
$ docker compose -f docker/compose.yaml up --build
$ ./scripts/smoke.sh
```

Вместе со всем стеком наблюдаемости — Prometheus, Loki, Tempo, Alloy и
Grafana (дашборды подхватываются автоматически):

```console
$ docker compose -f docker/compose.yaml -f docker/compose.observability.yaml up
```

Порты: Prometheus 9090, Loki 3100, Tempo 3200 (OTLP 4318), Alloy 12345,
Grafana 3000 (`admin` / `admin`).

Go, `gofmt` и `go vet` тоже проверяются без Docker:

```console
$ cd app && go test -race ./... && go vet ./...
```

## Пайплайн

`ci.yml` — четыре джобы:

| Джоба     | Что делает                                                       |
|-----------|------------------------------------------------------------------|
| `test`    | матрица linux/windows: gofmt, vet, `go test -race`, govulncheck     |
| `image`   | buildx + push в GHCR по коммиту и `main`, кэш GHA, SBOM/provenance |
| `security`| Trivy (CRITICAL/HIGH валят), SBOM CycloneDX, cosign sign + verify  |
| `staging` | pull образа по digest, compose up, дымовая проверка                |

`deploy.yml` запускается только вручную (`workflow_dispatch`) и только
через окружение `production`: там же секреты и ручное подтверждение.
Пайплайн превращает тег в digest и деплоит именно его, а после дымовой
проверки при неудаче откатывает на предыдущий образ из истории.

Честно про деплой: без VPS и секретов в окружении `production` воркфлоу
не падает, а честно пишет в лог, что стенд пропущен. Сейчас в этом
репозитории деплой по ssh не выполнялся — сервера нет; проверяется та
часть конвейера, которая от сервера не зависит (тесты, образ, скан,
подпись, стенд с дымом).

## Supply chain

- образ собирается в две стадии и финальный слой — distroless, без shell
  и пакетного менеджера, пользователь `nonroot`;
- в образ вшиты `version`, `commit` и `builtAt` — по `/info` видно, что
  именно запущено;
- Trivy валит сборку на CRITICAL/HIGH без исправлений;
- SBOM в формате CycloneDX сохраняется артефактом прогона;
- образ подписывается cosign в keyless-режиме (OIDC) и тут же
  проверяется с явным identity и издателем.

Гейт Trivy уже сработал на этом же проекте: при добавлении OTLP-экспортёра
подтянулся `google.golang.org/grpc`, и сборка упала на CVE-2026-84445.
Зависимость обновлена до исправленной версии — так это и должно выглядеть,
когда сканирование не для галочки.

## Инфраструктура

Terraform (`infra/terraform`) описывает стенд целиком: сервер, статический
IP, чтобы пересоздание машины не меняло точку входа, и облачный файрвол,
где ssh открыт только с перечисленных адресов, а наружу — 80 и 443.

```console
$ cd infra/terraform
$ cp terraform.tfvars.example terraform.tfvars   # адреса в git не кладём
$ terraform init && terraform plan -out=tfplan
$ terraform apply tfplan
```

Ansible (`infra/ansible`) готовит сервер после того, как тот создан:
пользователь для деплоя с sudo без пароля, автообновления безопасности,
fail2ban, docker с ограниченными логами и копия этого репозитория.

```console
$ cd infra/ansible
$ DEPLOY_HOST=203.0.113.10 ansible-playbook playbooks/provision.yml \
    -e base_ssh_public_key="$(cat ~/.ssh/id_ed25519.pub)"
```

Роли разделены по смыслу: `base` (пользователи и базовые пакеты),
`docker` (репозиторий и демон), `app` (копия репозитория и запуск
`deploy/deploy.sh`). Переменные ролей с префиксами (`base_*`, `app_*`,
`docker_*`) — этого требует ansible-lint, и заодно видно, кто какую
переменную объявил.

## Kubernetes

`k8s/charts/devops-lab` — helm-чарт с безопасными значениями по умолчанию:
поды непривилегированные, read-only корень, `cap_drop: ALL`, ограничены
ресурсы, liveness/readiness/startup бьют в `/healthz`, рядом HPA, PDB и
NetworkPolicy, который пускает к сервису только ingress-контроллер и
Prometheus.

```console
$ helm upgrade --install devops-lab k8s/charts/devops-lab \
    --set image.digest=sha256:... --set ingress.enabled=true
```

Образ задаётся по digest, а не тегом: тег `main` уезжает под новым
коммитом, digest остаётся с этим образом навсегда. `ServiceMonitor`
выключен по умолчанию — без Prometheus Operator в кластере установка с ним
не прошла бы.

## Наблюдаемость

Три столпа — метрики, логи и трейсы — и все три собираются прямо из
сервиса, без посредников.

### Метрики

Сервис отдаёт `/metrics`, `observability/prometheus` их разбирает, а
правила превращают сырые выражения в читаемые:

| Метрика                                   | Что означает                        |
|-------------------------------------------|-------------------------------------|
| `devops_lab_http_requests_total`          | запросы по маршруту и коду ответа    |
| `devops_lab_http_request_duration_seconds` | задержки, из них считается p95       |
| `devops_lab_build_info`                   | версия и коммит запущенного кода     |

```promql
job:devops_lab:http_error_ratio:rate5m        # доля 5xx
job:devops_lab:request_duration_seconds:p95_5m # p95 задержек
slo:devops_lab:success_ratio:rate1h           # SLI успешности
```

### Логи

Сервис пишет JSON в stdout, Alloy читает логи контейнеров и складывает их
в Loki. Метки вроде `{app="devops-lab"}` появляются из имени контейнера,
так что искать логи можно без всякой настройки:

```logql
{app="devops-lab"} | json | status >= 500
{app="devops-lab"} | json | route="GET /work"
{app="devops-lab"} | json | trace_id="<traceID>"
```

### Трейсы

Сервис шлёт спаны по OTLP в Tempo, а Tempo считает по ним RED-метрики и
граф сервисов и отдаёт их в Prometheus через remote write. Поэтому в Grafana
работают переходы из трейса в логи и в метрики, а в дереве видно дочерний
span `downstream.pricing` внутри `GET /work`.

### SLO и бюджет ошибок

Заявлен SLO 99% на успешность, то есть бюджет ошибок — 1% за окно. Расход
бюджета считается многокоочно: быстрый (окна 1 час и 5 минут, кратность
14.4) поднимает critical, медленный (6 часов и 30 минут, кратность 6) —
warning. Так «много ошибок подряд» и «медленная протечка» не смешиваются
в один алерт.

### Алерты и runbook'и

Алерты живут в `observability/prometheus/rules.yml`: сервис недоступен,
доля ошибок, высокий p95, подозрительная тишина в трафике, рестарты подов,
расход бюджета. В каждом аннотации `runbook` ссылается на
`observability/runbooks/*.md` — это те самые «сначала посмотри сюда, потом
дёргай деплой» шаги, которые обычно и отличают инженера от скрипта.

Дашборды в `observability/grafana/dashboards/`: метрики (`devops-lab`) и
логи (`devops-lab-logs`), оба подхватываются провижинингом.

## Проверки

`validate.yml` гоняет то, что нельзя проверить обычными тестами:

| Джоба     | Что делает                                                  |
|-----------|-------------------------------------------------------------|
| `terraform` | `fmt -check`, `init -backend=false`, `validate`             |
| `ansible`   | `--syntax-check` обоих плейбуков и `ansible-lint`            |
| `helm`      | `helm lint` и `helm template` со всеми опциями включёнными   |
| `prometheus`| `promtool check config` для конфига и правил                |
| `loki`      | `loki -verify-config` на конфиге Loki                          |
| `tempo`     | живой запуск Tempo и ожидание `/ready`                          |
| `alloy`     | `alloy validate` на конфиге агента                              |

## Что дальше

- [ ] релиз по тегам с changelog и semver
- [ ] ArgoCD-приложение для чарта, чтобы деплой шёл через git
- [ ] бэкап состояния стенда и проверка восстановления
- [ ] ночные сборки и проверка образа на устаревание базовых слоёв
- [ ] TLS terminates на реальном домене (сейчас ingress выключен)
