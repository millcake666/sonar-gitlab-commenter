# sonar-gitlab-commenter

`sonar-gitlab-commenter` это CLI-утилита, которая забирает проблемы из SonarQube и публикует их прямо в Merge Request GitLab:

- inline-дискуссии по проблемам, привязанным к файлу и строке
- один обновляемый summary-комментарий (quality gate, coverage, счетчики проблем)

Основной сценарий использования: запуск в GitLab CI в MR-пайплайнах.

## Для чего используется

- чтобы ревьюер видел замечания SonarQube непосредственно в MR
- чтобы не плодить новые summary-комментарии при каждом перезапуске пайплайна
- чтобы автоматически закрывать старые обсуждения, созданные этой утилитой
- чтобы проверить интеграцию в безопасном режиме `--dry-run` без записи в GitLab

## Как работает

При запуске утилита делает следующее:

1. Читает конфигурацию из переменных окружения и/или флагов CLI.
2. Проверяет контекст MR в GitLab (`project_id`, `mr_iid`) и получает `diff_refs`.
3. Проверяет авторизацию в SonarQube.
4. Загружает проблемы проекта из SonarQube.
5. Применяет фильтр по severity (если задан).
6. Загружает quality gate и метрики покрытия.
7. Если не `--dry-run`:
   - резолвит старые открытые дискуссии, созданные этой утилитой
   - публикует inline-дискуссии для проблем с привязкой к строке
   - создает или обновляет один summary-комментарий
8. Печатает action log в stdout.

Порядок severity: `INFO < MINOR < MAJOR < CRITICAL < BLOCKER`.

## Требования

- Go `1.22+` (если собираете из исходников)
- доступ раннера до API:
  - GitLab: `$GITLAB_URL/api/v4/...`
  - SonarQube: `$SONAR_HOST_URL/api/...`
- токены:
  - `GITLAB_TOKEN` с правами записи комментариев/дискуссий в MR
  - `SONAR_TOKEN` с правами чтения проблем и метрик проекта

## Установка

### Быстрая установка (Linux / macOS)

```bash
curl -sL https://raw.githubusercontent.com/millcake666/sonar-gitlab-commenter/main/install.sh | sh
```

Конкретная версия:

```bash
curl -sL https://raw.githubusercontent.com/millcake666/sonar-gitlab-commenter/main/install.sh | sh -s -- v1.0.0
```

Скрипт автоматически определяет ОС и архитектуру, скачивает нужный бинарник из GitHub Releases, проверяет `sha256` и устанавливает команду в `/usr/local/bin/sonar-gitlab-commenter`.

### GitHub Releases

Скачайте бинарник для своей платформы со страницы Releases:

| Файл | Платформа |
| --- | --- |
| `sonar-gitlab-commenter-linux-amd64` | Linux x86_64 |
| `sonar-gitlab-commenter-linux-arm64` | Linux ARM64 |
| `sonar-gitlab-commenter-darwin-amd64` | macOS Intel |
| `sonar-gitlab-commenter-darwin-arm64` | macOS Apple Silicon |
| `sonar-gitlab-commenter-windows-amd64.exe` | Windows x86_64 |

## Конфигурация

Флаги CLI имеют приоритет над переменными окружения.

### Переменные окружения

- `SONAR_HOST_URL` (обязательно)
- `SONAR_TOKEN` (обязательно)
- `SONAR_PROJECT_KEY` (обязательно)
- `GITLAB_URL` (обязательно)
- `GITLAB_TOKEN` (обязательно)
- `CI_PROJECT_ID` (обязательно)
- `CI_MERGE_REQUEST_IID` (обязательно)

### Флаги CLI

- `--sonar-url`
- `--sonar-token`
- `--sonar-project-key`
- `--severity-threshold` (`INFO|MINOR|MAJOR|CRITICAL|BLOCKER`)
- `--dry-run`
- `--gitlab-url`
- `--gitlab-token`
- `--project-id`
- `--mr-iid`

## Пример локального запуска

```bash
go build -o sonar-gitlab-commenter .

SONAR_HOST_URL="https://sonar.example.com" \
SONAR_TOKEN="$SONAR_TOKEN" \
SONAR_PROJECT_KEY="my-project-key" \
GITLAB_URL="https://gitlab.example.com" \
GITLAB_TOKEN="$GITLAB_TOKEN" \
CI_PROJECT_ID="123" \
CI_MERGE_REQUEST_IID="45" \
./sonar-gitlab-commenter --severity-threshold MAJOR
```

Проверка без записи в GitLab:

```bash
./sonar-gitlab-commenter --dry-run
```

## Примеры GitLab CI job в пайплайне

Используйте только в MR-пайплайнах, потому что утилите нужен `CI_MERGE_REQUEST_IID`.

### Основной job для MR

```yaml
sonar_mr_comment:
  image: alpine:3.20
  stage: quality
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
  variables:
    GITLAB_URL: "$CI_SERVER_URL"
    GITLAB_TOKEN: "$GITLAB_API_TOKEN"   # masked variable
    SONAR_HOST_URL: "$SONAR_HOST_URL"   # masked variable
    SONAR_TOKEN: "$SONAR_TOKEN"         # masked variable
    SONAR_PROJECT_KEY: "my-project-key"
  script:
    - apk add --no-cache curl
    - curl -sL https://raw.githubusercontent.com/millcake666/sonar-gitlab-commenter/main/install.sh | sh
    - sonar-gitlab-commenter --severity-threshold MAJOR
```

### Ручной dry-run job

```yaml
sonar_mr_comment_dry_run:
  image: alpine:3.20
  stage: quality
  rules:
    - if: '$CI_PIPELINE_SOURCE == "merge_request_event"'
      when: manual
  variables:
    GITLAB_URL: "$CI_SERVER_URL"
    GITLAB_TOKEN: "$GITLAB_API_TOKEN"
    SONAR_HOST_URL: "$SONAR_HOST_URL"
    SONAR_TOKEN: "$SONAR_TOKEN"
    SONAR_PROJECT_KEY: "my-project-key"
  script:
    - apk add --no-cache curl
    - curl -sL https://raw.githubusercontent.com/millcake666/sonar-gitlab-commenter/main/install.sh | sh
    - sonar-gitlab-commenter --dry-run --severity-threshold CRITICAL
```

## Пример вывода

```text
Action log: found 12 issues, published 9 comments
Resolved 3 previous SonarQube discussions in merge request 45
Posted 8 inline SonarQube discussions to merge request 45
Updated summary SonarQube note in merge request 45
Quality gate: passed, coverage: 81.20%, new code coverage: 76.40%
Resolved GitLab merge request: project_id=123, mr_iid=45
```

## Важные детали

- Inline-дискуссии создаются только для проблем с `file path` и `line`.
- Проблемы без привязки к строке попадают в summary-комментарий.
- Повторный запуск обновляет summary и закрывает старые tool-комментарии.
- Текущая реализация использует общий таймаут `30s` на один запуск.
