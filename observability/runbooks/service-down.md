# devops-lab недоступен

Алерт `DevopsLabDown`: `up{job="devops-lab"} == 0` держится дольше двух минут.

## 1. Понять, где обрыв

```console
$ curl -i http://<host>/healthz          # отвечает ли сервис вообще
$ docker compose -f docker/compose.yaml logs --tail 100 app
$ kubectl get pods -l app.kubernetes.io/name=devops-lab
$ kubectl describe pod <pod> | tail -30  # события: OOMKilled, ImagePullBackOff...
```

## 2. Типовые причины

| Симптом | Причина | Что делать |
|---------|---------|------------|
| `ImagePullBackOff` | неверный digest или нет доступа к GHCR | проверить `image.digest` в values, `imagePullSecrets` |
| `OOMKilled` | превышен лимит памяти | поднять `resources.limits.memory` |
| `CrashLoopBackOff` | сервис падает на старте | `kubectl logs <pod> --previous` |
| отвечает, но `up = 0` | Prometheus не ходит по `/metrics` | проверить `targets` в Prometheus, порт в Service |

## 3. Откат

Если сломался последний деплой:

```console
$ cd infra/ansible
$ ansible-playbook playbooks/deploy.yml -e "image_ref=<прошлый digest>"
```

или из CI: Actions → deploy → Run workflow с предыдущим образом.
