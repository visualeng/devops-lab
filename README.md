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
app/                  сервис-подопытный на Go: healthz, info, дренирование
docker/compose.yaml   локальный стенд: собрать и поднять
docker/compose.ci.yaml   стенд в CI: чужой образ по digest, ничего не собирает
docker/compose.prod.yaml  прод: digest вместо тега, read-only, cap_drop, лимит логов
deploy/deploy.sh      деплой на сервере: pull по digest, up --wait, запись состояния
deploy/rollback.sh    откат на предыдущий digest из истории
scripts/smoke.sh      дымовая проверка: /healthz, /info, 404 на неизвестный путь
infra/terraform/      VPS как код: сеть, диск, firewall, ssh-ключ  (этап 2)
infra/ansible/        провижининг и деплой на сервере               (этап 2)
k8s/                  chart/kustomize и ArgoCD-подготовка             (этап 3)
observability/        Prometheus-алерты, дашборд, runbook            (этап 3)
```

## Сервис

Три ручки и никакой базы — весь фокус на конвейере:

```console
$ curl localhost:8080/healthz
{"status":"ok"}

$ curl localhost:8080/info
{"builtAt":"2026-09-28T11:40:56Z","commit":"b8936c5c44f9a7b5ef582abab24df91907195bc6","service":"devops-lab","version":"main"}
```

`/info` отдаёт ровно тот коммит, который был в сборке образа, — этим
пользуются и дымовая проверка (сверяет с SHA коммита), и деплой (проверяет,
что на сервере именно то, что просили). Сам сервис умеет проверять своё
здоровье ключом `-healthcheck`: в финальном образе нет ни shell, ни curl,
поэтому healthcheck выполняет бинарник.

## Локальный запуск

Нужен только Docker:

```console
$ docker compose -f docker/compose.yaml up --build
$ ./scripts/smoke.sh
```

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

## Что дальше

- [ ] **этап 2** — Terraform (VPS: сеть, диск, firewall, ssh-ключ) и
      Ansible (пользователи, docker, репозиторий на сервере, бэкапы)
- [ ] **этап 3** — k8s-манифесты: chart или kustomize, ArgoCD-ready,
      Prometheus-алерты и runbook
- [ ] релиз по тегам с changelog и semver
- [ ] ночные сборки и проверка образа на устаревание базовых слоёв
