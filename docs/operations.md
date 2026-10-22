# Эксплуатация AIQ Core

Быстрый запуск и первый администратор: [README](../README.md). Приведённые Docker-команды выполняются из корня проекта в PowerShell. Для Linux/macOS команды `docker compose exec/run` те же; переменные задаются через `export`, а метку копии — через `date +%Y%m%d-%H%M%S`.

Ежедневная работа: [инструкция администратора](admin-guide.md). Передача и выпуск:
[R14](handover-r14.md), [чек-лист](acceptance-r14.md). Фактические локальные
испытания и неподтверждённые условия стенда: [отчёт R13](r13-validation-report.md).

## Конфигурация

| Переменная | Назначение | Значение по умолчанию |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | Пароль БД для Compose | Нет; задать случайный пароль вместо шаблона |
| `DATABASE_URL` | Подключение Go к PostgreSQL | Нет; обязательна |
| `APP_ORIGIN` | Точный origin браузера, включая схему/порт | В Compose `http://localhost:8080`; вне Compose обязательна для сервера |
| `HTTP_ADDR` | Адрес HTTP-сервера | `:8080` |
| `TRUST_PROXY` | Использовать доверенный X-Real-IP для лимита входов | `false`; в закрытом backend Compose — `true` |
| `ADMIN_EMAIL`, `ADMIN_NAME`, `ADMIN_PASSWORD` | Однократный bootstrap администратора | Нет; нужны только команде admin |
| `TEST_DATABASE_URL` | Отдельная БД для интеграционных тестов | Нет; без неё тесты интеграции пропускаются |

В Git хранится только `.env.example`. `.env` и его варианты, `.pgpass`, архивы `*.dump`, `backups/` игнорируются; Docker build contexts исключают env-файлы и копии. Вне Compose Go не читает `.env` автоматически. Передавайте секреты окружением процесса; пароль администратора вводите скрыто, как в README. `cmd/admin` удаляет `ADMIN_PASSWORD` из своего окружения после чтения.

В Compose PostgreSQL доступен только на `127.0.0.1:5432`, frontend — на `127.0.0.1:8080`; backend не публикует порт на хост. Для разработки Vite на `localhost:5173` проксирует API к Go на порту 8080. Указывайте соответствующий `APP_ORIGIN`. `localhost` и `127.0.0.1` не взаимозаменяемы для CSRF.

Для публичного HTTPS используйте отдельный `compose.pilot.yaml`, описанный ниже. Он завершает TLS непосредственно в nginx frontend; дополнительный внешний proxy не требуется. `APP_ORIGIN=https://ваш-домен` включает Secure cookies. `TRUST_PROXY=true` используйте только при недоступности backend напрямую из недоверенной сети.

## Миграции и обновление

1. Сделать резервную копию текущей БД.
2. Прочитать новые SQL-миграции и проверить обновление на копии БД.
3. На время изменения схемы остановить backend, сохраняя запущенную БД.
4. Применить миграции нового образа и запустить приложение.
5. Проверить readiness, вход и основные списки.

```powershell
docker compose stop backend
docker compose run --rm --build migrate
if ($LASTEXITCODE -ne 0) { throw 'Миграция не выполнена; приложение не запускается' }
docker compose up --build -d backend frontend
Invoke-RestMethod http://localhost:8080/health/ready
```

Без Docker: из `backend` выполнить `go run ./cmd/migrate` при заданной `DATABASE_URL`. Команда не требует `APP_ORIGIN` или учётных данных администратора. Сервер и bootstrap также автоматически применяют миграции, но отдельная команда удобна при обслуживании.

`database.Migrate` сортирует встроенные SQL-файлы, берёт транзакционный advisory lock и применяет только отсутствующие в `schema_migrations`. Все ожидающие миграции коммитятся вместе. При ошибке изменения схемы и новые номера откатываются, процесс завершается ненулевым кодом. Повторный запуск не повторяет выполненные миграции. Проверить версии:

```powershell
docker compose exec -T db psql -U aiq -d aiq -c 'SELECT name, applied_at FROM schema_migrations ORDER BY name;'
```

Текущая последовательность: 001 auth, 002 teachers, 003 families, 004 programs, 005 groups, 006 lessons, 007 attendance, 008 student360, 009 tasks, 010 audit_log, 011 tasks_lifecycle, 012 lesson_history, 013 snapshot_correction_history, 014 reschedule_reason, 015 task_closure_policy, 016 lesson_participants. Не изменяйте применённые файлы: добавляйте следующий номер и собирайте новые бинарники. Роли БД нужны права на схему, таблицы и установку `btree_gist`. При отсутствии расширения согласуйте его установку с администратором PostgreSQL.

Перед 016 остановите старые backend-процессы и создайте резервную копию. После разделения таблиц старый бинарник не совместим со схемой; запускается новая версия. ID и история сохраняются, runner выполняет миграцию атомарно и не повторяет применённую версию. Проверки отказа/повторного запуска и структуры: [схема БД](database-schema.md).

Автоматического отката миграций нет. Старую версию приложения не запускают на изменённой схеме без проверки совместимости. Для отката разворачивают резервную копию в отдельную БД и совместимую версию приложения. `docker compose down` сохраняет том; `down -v` удаляет данные и не используется при обычном обслуживании.

## Резервные копии

Используется отдельный процесс R12: ежедневно в 03:00 Europe/Moscow, семь
успешных копий, временный файл, полное чтение архива, SHA-256 и публикация через
атомарное переименование. Ошибки сохраняют предыдущие копии и дают failed/unhealthy.
Ротация выполняется только после сохранения новой копии. Статус хранится отдельно
от архива, устаревание более 26 часов считается сбоем.

[Инструкция R12](backup-r12.md) определяет расписание, параметры и защиту отдельного
хранилища, права роли/файлов, запуск Compose/systemd, контроль успешности, действия
при ошибках, подключение внешних уведомлений и порядок пробного восстановления.
[Отчёт проверки](r12-backup-report.md) отделяет локальное испытание от приёмки
инфраструктуры эксплуатации.

## Восстановление

Приёмочный путь — `backup restore ID` из [инструкции R12](backup-r12.md).
Он проверяет контрольную сумму, создаёт новую пустую БД со случайным именем,
восстанавливает архив одной транзакцией и сверяет все таблицы и миграции.
После этого отдельный экземпляр приложения направляют на восстановленную БД
и проверяют readiness, вход, связи, учебную историю, посещаемость, задачи и audit.
Рабочая БД не перезаписывается. В отчёт включают дату копии, версии, размер,
длительность и результаты каждого контроля; успех CLI без проверки приложения
недостаточен. Bootstrap на восстановленных аккаунтах не повторяют.

Для фактического переключения остановите записи в рабочее приложение, восстановите выбранную актуальную копию и направьте backend на проверенную БД. Стандартный Compose использует имя `aiq` в DATABASE_URL. Создайте локальный `.cache/compose.restore.yaml` с переопределением подключения для backend и служебных команд:

```yaml
services:
  backend:
    environment:
      DATABASE_URL: postgres://aiq:${POSTGRES_PASSWORD}@db:5432/aiq_restore?sslmode=disable
  migrate:
    environment:
      DATABASE_URL: postgres://aiq:${POSTGRES_PASSWORD}@db:5432/aiq_restore?sslmode=disable
  admin:
    environment:
      DATABASE_URL: postgres://aiq:${POSTGRES_PASSWORD}@db:5432/aiq_restore?sslmode=disable
```

```powershell
docker compose stop backend
docker compose -f compose.yaml -f .cache/compose.restore.yaml up -d --force-recreate backend frontend
Invoke-RestMethod http://localhost:8080/health/ready
```

Пароль подставляется из окружения/локального `.env`; сам override не содержит его значения. Для дальнейшего обслуживания этой восстановленной среды используйте те же два `-f`, в том числе при запуске migrate. Предыдущую БД сохраняйте до проверки результата. Изменения после момента резервной копии сами не восстановятся.

При восстановлении рабочей среды отзовите сохранённые в копии сессии по внутреннему порядку администрирования и снова войдите. Пробную среду из рабочей копии изолируйте от публичного доступа и демонстрационной среды.

## Health и диагностика

| Endpoint | Успех | Ошибка |
| --- | --- | --- |
| `GET /health/live` | `200`, процесс обрабатывает запрос | Процесс/сеть недоступны |
| `GET /health/ready` | `200`, pool.Ping БД успешен | `503 not_ready`, таймаут проверки БД 2 секунды |

Live намеренно не зависит от БД, чтобы кратковременный сбой БД не вызывал перезапуск живого процесса. Оба endpoint доступны без входа, возвращают JSON и X-Request-ID. При недоступности самого backend nginx возвращает `503 unavailable`.

```powershell
Invoke-RestMethod http://localhost:8080/health/live
Invoke-RestMethod http://localhost:8080/health/ready
docker compose logs --tail 100 backend frontend
```

Логи backend и nginx структурированы; ищите запрос по `request_id` из ответа. Backend пишет шаблон маршрута, статус, длительность. Исходные ошибки, SQL-параметры и конфигурация с секретами не печатаются; при ошибке БД доступен SQLSTATE. Нативный nginx error log отключён, поскольку содержит адреса и URL; сбои upstream видны по структурированному access log, health и логам backend.

При ошибке запуска проверьте обязательные переменные, доступность БД, права и версии миграций. При `403` проверьте APP_ORIGIN, CSRF и роли; при `409` — уникальность связей, вместимость и пересечения времени; при `413` — размер пакета. Лимиты JSON backend: обычно 32 КиБ, задачи 64 КиБ, массовая посещаемость 1 МиБ; nginx допускает 1 МиБ и оставляет меньшие проверки backend.

## Проверки перед передачей

Команды Go, frontend build и Playwright — в README. Интеграционные тесты требуют отдельного `TEST_DATABASE_URL` и создают временные схемы; не используйте рабочую БД. UI-тесты подменяют API вымышленными ответами, а настоящие транзакции и ограничения проверяют Go-тесты с PostgreSQL.

Контейнерная проверка в среде с Docker:

```powershell
docker compose config --quiet
docker compose up --build -d
docker compose exec -T frontend nginx -t
Invoke-RestMethod http://localhost:8080/health/ready
```

Не публикуйте вывод обычного `docker compose config` с подставленными секретами. При сдаче дополнительно проверьте создание первого администратора на отдельной пустой БД, резервное копирование и полное пробное восстановление. Фактически проведённые проверки и ограничения окружения фиксируются в [validation.md](validation.md).

## HTTPS-пилот R11

Это инструкция запуска, а не подтверждение публикации. Фактически выполненные проверки и оставшиеся условия приёмки приведены в [отчёте R11](r11-operations-report.md). Базовый `compose.yaml` предназначен для локального HTTP; пилот запускается **только** через самостоятельный `compose.pilot.yaml`. Не объединяйте эти файлы как base/override.

### Изолированная контейнерная проверка

Нужны работающий Docker Engine с Linux-контейнерами, Compose v2 и Node.js 22.12+. Go на хосте не требуется: одноразовый сертификат генерируется в закреплённом Go build image.

```powershell
node scripts/verify-r11.mjs
```

Скрипт выбирает свободные локальные порты, создаёт уникальный проект `aiq-r11-*` и собственный пустой том БД. Рабочие `.env`, БД и проект `aiq-core` он не использует. Пароли генерируются в памяти. Сертификат на 24 часа предназначен только для localhost; клиент явно доверяет ему, сохраняя проверку подписи и имени. Публично доверенный сертификат пилота этим не заменяется.

Проверка выполняет `compose config --quiet`, `build --pull`, `up db`, migrate дважды с неизменностью `schema_migrations`, bootstrap admin, `up backend frontend`, `nginx -t`, вход, HTTPS/cookies/CSRF, лимит 1 МиБ. Затем действительно останавливает БД и backend, проверяет разные 503 и восстановление, выход и отсутствие проверочных контактов/секретов в логах. В конце удаляет только созданные им контейнеры и том. Запускайте проверку до загрузки рабочих данных; она не предназначена для подключения к существующей БД.

Результат: `.cache/aiq-r11-*/report.json`, где должны быть `status: passed`, все checks и успешная очистка. В отчёт входят время, последовательность команд без значений секретов, версии Docker/Compose, image ID и доступные RepoDigests. При отсутствии Docker отчёт получает `blocked`; это не успешная проверка. В CI тот же файл публикуется артефактом задачи `docker-https`. Само наличие задачи не подтверждает её успешный запуск.

### Подготовка сервера и TLS

Нужен согласованный Linux-сервер с Docker/Compose, домен с корректными A/AAAA и доступными 80/443, действительный сертификат для этого домена. Порт SSH ограничивают доступом администратора. PostgreSQL и backend в пилотном Compose не имеют host ports; наружу публикуется только nginx. Не запускайте пилот с `network_mode: host` и не добавляйте публикацию 5432/8080.

Скопируйте `deploy/pilot.env.example` в `.env.pilot`, задайте случайный буквенно-цифровой `POSTGRES_PASSWORD`, фактические `PILOT_HOST`, `APP_ORIGIN` без завершающего `/` и абсолютный `TLS_CERTS_DIR`. Для обычного 443 origin имеет вид `https://pilot.example.org`; примерный домен необходимо заменить. Ограничьте права файла владельцем (`chmod 600 .env.pilot`). Не сохраняйте там `ADMIN_PASSWORD`, не публикуйте `.env.pilot` или вывод `docker compose config` без `--quiet`.

Сертификат выдаётся средствами инфраструктуры/ACME до запуска пилота. В каталоге `TLS_CERTS_DIR` должны находиться обычные файлы `fullchain.pem` и `privkey.pem`. Каталог `/etc/letsencrypt/live/...` содержит ссылки на соседний archive: его отдельное монтирование ломает эти ссылки. Например, на Linux скопируйте выданные файлы с разыменованием ссылок в отдельный каталог:

```bash
sudo install -d -m 700 /srv/aiq/tls
sudo install -m 644 /etc/letsencrypt/live/pilot.example.org/fullchain.pem /srv/aiq/tls/fullchain.pem
sudo install -m 600 /etc/letsencrypt/live/pilot.example.org/privkey.pem /srv/aiq/tls/privkey.pem
```

Здесь `pilot.example.org` — заменяемый пример. Ключ не помещают в репозиторий или build context. После каждого обновления сертификата нужно обновить оба файла в этом каталоге, выполнить `nginx -t` и reload, как ниже; без deploy hook ACME обновление файлов в исходном live-каталоге само по себе не обновит nginx. Успешное автоматическое продление следует проверить отдельно на целевом сервере.

### Первый запуск на пустой БД

Ниже команды для shell Linux из корня проекта. В PowerShell те же команды `docker compose --env-file .env.pilot -f compose.pilot.yaml ...`; скрытый ввод пароля показан в README — замените там вызов Compose, добавив эти два параметра. На новой установке:

```bash
set -e
pilot() { docker compose --env-file .env.pilot -f compose.pilot.yaml "$@"; }
pilot config --quiet
pilot build --pull backend frontend
pilot up -d --wait db
pilot run --rm -T migrate
pilot run --rm -T migrate
read -r -p 'Email первого администратора: ' ADMIN_EMAIL
read -r -p 'Имя администратора: ' ADMIN_NAME
read -r -s -p 'Пароль (12+ символов, до 72 байт UTF-8): ' ADMIN_PASSWORD
printf '\n'
export ADMIN_EMAIL ADMIN_NAME ADMIN_PASSWORD
trap 'unset ADMIN_EMAIL ADMIN_NAME ADMIN_PASSWORD' EXIT
pilot run --rm -T -e ADMIN_EMAIL -e ADMIN_NAME -e ADMIN_PASSWORD admin
unset ADMIN_EMAIL ADMIN_NAME ADMIN_PASSWORD
trap - EXIT
pilot up -d --wait backend frontend
pilot exec -T frontend nginx -t
curl --fail --show-error https://pilot.example.org/health/live
curl --fail --show-error https://pilot.example.org/health/ready
```

Bootstrap создаёт первого администратора только в пустой системе. Пароль вводит сам владелец через скрытый ввод; не передавайте его аргументом команды. При ошибке сценарий останавливается: не продолжайте публикацию до исправления. Повтор bootstrap не используется для сброса пароля. После запуска откройте действительный HTTPS-адрес, войдите и проверьте фактическую роль admin.

### Проверка и передача пилота

До загрузки рабочих данных проверьте из внешней сети страницу и health, доверие браузера к сертификату, перенаправление HTTP, точный Origin, обе Secure/HttpOnly/SameSite=Strict cookies. Не копируйте значения cookies или CSRF-токенов в отчёт. Проверки с неверным Origin/CSRF должны возвращать 403; тело более 1 МиБ — общую JSON-ошибку 413. Для реальных отказов и восстановления в отдельное окно:

```bash
pilot stop -t 5 db
# /health/ready: 503 not_ready; /health/live: 200
pilot start --wait db
curl --fail --show-error https://pilot.example.org/health/ready
pilot stop -t 5 backend
# API и /health/ready через nginx: 503 unavailable
pilot start --wait backend
curl --fail --show-error https://pilot.example.org/health/ready
pilot exec -T frontend nginx -t
```

Проверьте каждый ожидаемый статус HTTP и `code`, `message`, `field_errors`, `request_id`, совпадение request ID с заголовком. Просмотрите логи по request ID без вывода чувствительных заголовков/тел, проверьте снаружи недоступность БД/backend, сохраните версии Docker/Compose и image ID/RepoDigests. Не прикладывайте полный `docker inspect`: он содержит environment с паролем БД.

После продления сертификата:

```bash
pilot exec -T frontend nginx -t
pilot exec -T frontend nginx -s reload
```

В акт передачи внесите настоящий HTTPS-адрес, дату, проверенные версии/ID образов, результаты каждого шага, ответственного за сертификат и резервные копии. Первый владелец вводит свой пароль сам; дополнительные администраторы получают временный пароль отдельно через согласованный безопасный канал и меняют его при входе. До фактического запуска и передачи доступа R11 остаётся незавершённым.
