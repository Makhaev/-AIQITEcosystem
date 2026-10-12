# AIQ Core

Реализованы авторизация, пользователи, серверные проверки ролей, педагоги, ученики, родители, программы, модули, группы, зачисления, расписание и сохранённый состав журнала. Backend — Go и `net/http`, модульный монолит, PostgreSQL и pgx без ORM. Frontend — React, TypeScript и Vite. Модули разделяют HTTP, валидацию и операции БД; проверка объединённых полей PATCH выполняется внутри транзакции.

## Что работает

- Вход по email и паролю, выход, смена собственного пароля.
- Создание пользователей администратором, список с пагинацией, изменение имени и ролей, блокировка, разблокировка и сброс пароля.
- Педагоги: создание и редактирование администратором, профиль, поиск по ФИО без учёта регистра, пагинация, фильтр по статусу, архивирование и восстановление, необязательная уникальная связь с учётной записью. Руководитель читает все профили; педагог — только собственный, включая проверку по ID.
- Ученики и родители: карточки, поиск, пагинация, редактирование, архив и восстановление. Необязательная дата рождения и куратор; у родителя обязателен телефон или email. Связи многие-ко-многим содержат представительство и основной контакт; повторные пары и второй основной контакт запрещены в PostgreSQL. Куратор читает только закреплённых учеников и их родителей, включая фильтрацию детей в общей карточке родителя. [Контракт и правила](docs/students-and-parents.md).
- Программы: название, описание, статусы draft/active/archived, поиск, пагинация, редактирование и архив. Модули: создание/изменение, уникальные номера внутри программы и упорядоченный список. Новая связь группы требует активной программы; составные FK запрещают занятие с модулем другой программы. [Контракт, права и ограничения](docs/programs-and-modules.md).
- Группы: создание, редактирование, карточка и архив; зачисление и вывод с датами и сохранением истории. PostgreSQL запрещает повторное активное зачисление и переполнение при параллельных запросах; перевод выполняется в транзакции и сохраняет посещаемость. [Контракт и ограничения](docs/groups-and-enrollments.md).
- Расписание: занятия, необязательный модуль, статусы, московское время и фильтры. Ограничения PostgreSQL запрещают пересечения группы или педагога, включая параллельные запросы; отменённые занятия не занимают интервал. Состав журнала фиксируется на дату занятия при первом открытии; администратор исправляет его с причиной и audit, сохраняя прежние отметки. [Расписание и журнал](docs/lessons-and-journals.md).
- Общая матрица прав с запретом по умолчанию и объектными ограничениями: [роли и педагоги](docs/roles-and-teachers.md).
- Роли `admin`, `leader`, `teacher`, `curator`. Управление пользователями доступно только `admin`. Проверка выполняется сервером.
- Уникальный email без учёта регистра; bcrypt с cost 10. Пароли — минимум 12 Unicode-символов, максимум 72 байта UTF-8.
- Сессии на 12 часов в PostgreSQL. Случайные токены по 256 бит; в БД сохраняются SHA-256 хеши. В браузере — host-only `HttpOnly`, `SameSite=Strict` cookies; `Secure` включается при HTTPS в `APP_ORIGIN`.
- CSRF для всех изменяющих запросов, включая вход: точная проверка `Origin`, cookie и `X-CSRF-Token`; после входа токен дополнительно привязан к серверной сессии.
- Выход отзывает текущую сессию; смена/сброс пароля, блокировка и изменение ролей отзывают все сессии пользователя.
- Создание и изменение пользователей повторно проверяют сессию и права администратора внутри транзакции. Истечение сессии во время ожидания блокировки также прекращает доступ.
- Временные пароли необходимо сменить при входе. До смены доступны только `/auth/me`, `/auth/logout`, `/auth/change-password` и получение CSRF-токена.
- Защита последнего активного администратора, ограничение попыток входа, базовый audit авторизации и изменений пользователей.
- `/health/live`, `/health/ready`, миграции при запуске, структурированные логи без тел запросов и секретов.

Публичной регистрации и почтового восстановления пароля нет. Ученики и родители не имеют собственных кабинетов. API задач и показателей ещё не реализованы; их frontend-экраны показывают явное состояние недоступности. Для расписания требуется расширение PostgreSQL `btree_gist`, устанавливаемое миграцией `006_lessons.sql`. Общая тема и компоненты соответствуют брендбуку AIQ: [дизайн-система и проверки](docs/frontend/design-system.md).

## Запуск через Docker Compose

Нужен Docker с Compose. Из корня проекта:

```powershell
Copy-Item .env.example .env
```

В `.env` замените `POSTGRES_PASSWORD` случайным паролем из букв и цифр (он используется в URL подключения). `APP_ORIGIN` должен точно совпадать с адресом браузера, включая порт; `localhost` и `127.0.0.1` — разные origins.

```powershell
docker compose up --build -d
```

Откройте `http://localhost:8080`. Перед первым входом создайте администратора отдельной командой.

## Первый администратор

Команда создаёт первого администратора только в пустой системе и не меняет существующие аккаунты. Пароля по умолчанию нет. Для PowerShell пароль вводится скрыто и не попадает в историю команд:

```powershell
$env:ADMIN_EMAIL = 'admin@your-domain.ru'
$env:ADMIN_NAME = 'Администратор AIQ'
$adminSecure = Read-Host 'Пароль администратора (минимум 12 символов)' -AsSecureString
$adminPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($adminSecure)
try {
    $env:ADMIN_PASSWORD = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($adminPointer)
    docker compose run --rm -e ADMIN_EMAIL -e ADMIN_NAME -e ADMIN_PASSWORD admin
} finally {
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($adminPointer)
    Remove-Item Env:ADMIN_PASSWORD -ErrorAction SilentlyContinue
    Remove-Item Env:ADMIN_EMAIL -ErrorAction SilentlyContinue
    Remove-Item Env:ADMIN_NAME -ErrorAction SilentlyContinue
}
```

Для запуска backend вне Docker используется та же команда `go run ./cmd/admin` из папки `backend`, с установленной `DATABASE_URL`. Замените только строку `docker compose run …` в приведённом примере.

## Разработка без контейнеров приложения

Требуются Go 1.24+ и PostgreSQL 17. Для frontend рекомендуется Node.js 22.12+. В dev-зависимостях закреплён локальный Node.js 22, который используется npm-скриптами. На Windows добавлена явная optional-зависимость нативного сборщика Vite; на Linux npm её пропускает.

Можно поднять только БД: `docker compose up -d db`. В терминале backend:

```powershell
$env:DATABASE_URL = 'postgres://aiq:YOUR_DB_PASSWORD@127.0.0.1:5432/aiq?sslmode=disable'
$env:APP_ORIGIN = 'http://localhost:5173'
Set-Location backend
go mod download
go run ./cmd/server
```

В отдельном терминале из корня:

```powershell
Set-Location frontend
npm ci
npm run dev
```

Откройте `http://localhost:5173`. Vite проксирует `/api` и `/health` к Go на порту 8080. Backend не загружает `.env` автоматически: вне Compose переменные задаются в окружении.

Для HTTPS укажите `APP_ORIGIN=https://ваш-домен` и настройте TLS на внешнем reverse proxy. В Compose `TRUST_PROXY=true`, поскольку backend не опубликован на хосте и принимает запросы через nginx. Nginx заменяет `X-Real-IP` адресом клиента для ограничения входов. При прямом доступе к backend оставьте `TRUST_PROXY=false`; включайте доверие только для backend, доступного исключительно через контролируемый proxy.

## API

Контракт: [docs/openapi.yaml](docs/openapi.yaml). Префикс `/api/v1`.

| Метод | Маршрут | Назначение |
| --- | --- | --- |
| GET | `/auth/csrf` | Получить CSRF-токен |
| POST | `/auth/login` | Вход |
| GET | `/auth/me` | Текущий пользователь |
| POST | `/auth/logout` | Выход |
| POST | `/auth/change-password` | Смена пароля |
| GET | `/users` | Список пользователей, только admin |
| POST | `/users` | Создание пользователя, только admin |
| PATCH | `/users/{id}` | Имя, роли, блокировка, временный пароль, только admin |
| GET | `/teachers` | Список, поиск, статус и пагинация с учётом прав |
| GET | `/teachers/{id}` | Профиль: admin/leader — любой, teacher — собственный |
| POST | `/teachers` | Создание профиля, только admin |
| PATCH | `/teachers/{id}` | Изменение, архивирование и восстановление, только admin |
| GET | `/students` | Список, поиск, статус и пагинация с учётом прав |
| POST | `/students` | Создать карточку ученика, только admin |
| GET | `/students/{id}` | Карточка с родителями: admin/leader — любая, curator — закреплённая |
| PATCH | `/students/{id}` | Изменить карточку, куратора, архивировать или восстановить, только admin |
| GET | `/parents` | Поиск по ФИО и контактам, статус и пагинация с учётом прав |
| POST | `/parents` | Создать родителя с хотя бы одним контактом, только admin |
| GET | `/parents/{id}` | Карточка родителя; связанные ученики отфильтрованы по правам |
| PATCH | `/parents/{id}` | Изменить, архивировать или восстановить, только admin |
| POST | `/students/{id}/parents` | Добавить семейную связь, только admin |
| PATCH | `/students/{id}/parents/{parent_id}` | Изменить представительство или основной контакт, только admin |
| GET | `/programs` | Список, поиск и пагинация программ с учётом связей и прав |
| POST | `/programs` | Создать программу, только admin |
| GET | `/programs/{id}` | Программа с упорядоченными модулями |
| PATCH | `/programs/{id}` | Изменить программу, статус, архивировать или восстановить, только admin |
| POST | `/programs/{id}/modules` | Добавить модуль со свободным номером, только admin |
| GET | `/programs/{id}/modules/{module_id}` | Модуль с проверкой прав и принадлежности программе |
| PATCH | `/programs/{id}/modules/{module_id}` | Изменить название, описание или номер модуля, только admin |

API-клиент сохраняет cookies из `/auth/csrf`, передаёт `Origin: APP_ORIGIN` и `X-CSRF-Token` при POST/PATCH. После входа используется новый `csrf_token` из ответа. Сессионный токен передаётся только через cookie. Ошибка содержит `code`, `message`, `field_errors`, `request_id`. Изменение email текущим API не предусмотрено.

## Проверки

```powershell
Set-Location backend
go test ./...
go vet ./...
go build ./cmd/server ./cmd/admin
```

Для интеграционных тестов задайте `TEST_DATABASE_URL` к тестовой БД. Каждый тест создаёт отдельную схему `aiq_test_*` и удаляет только её после проверки. Пользователю БД нужны права создания схем. Без переменной интеграционные тесты явно пропускаются; в CI переменная обязательна и настроена.

```powershell
$env:TEST_DATABASE_URL = 'postgres://aiq:YOUR_DB_PASSWORD@127.0.0.1:5432/aiq_test?sslmode=disable'
go test -count=1 ./...
```

Сборка frontend:

```powershell
Set-Location frontend
npm ci
npm run build
```

CI проверяет интеграцию с PostgreSQL, `go test -race`, `go vet`, сборку Go и frontend. Локальные результаты: [docs/validation.md](docs/validation.md).

## Структура

```text
backend/
  cmd/server/          HTTP-сервер
  cmd/admin/           Одноразовое создание администратора
  internal/modules/
    auth/              handler, service, repository
    users/             handler, service, repository
  internal/platform/   БД, миграции, конфигурация, HTTP-ответы
  migrations/          SQL-миграции, включённые в бинарник
frontend/              React, TypeScript, Vite и nginx
docs/                  Исходные документы, OpenAPI, результаты проверок
compose.yaml
```

Справочные материалы: [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [требования Vite к Node.js](https://vite.dev/guide/).
