# Organization Structure API

REST API для управления организационной структурой: иерархические подразделения и сотрудники.

## Стек

- **Go 1.22** — стандартный `net/http` с pattern-based маршрутизацией (метод + путь + path-параметры)
- **PostgreSQL 16** — основное хранилище
- **GORM** — ORM-слой
- **goose** — миграции схемы
- **Docker + docker-compose** — упаковка и локальный запуск
- **testify** — ассерты в тестах, `net/http/httptest` — интеграционные HTTP-тесты
- **log/slog** — структурированное логирование (JSON в проде, текст локально)

## Запуск

```bash
docker compose up --build
```

После старта API доступен на `http://localhost:8080`, Postgres проброшен на `localhost:55432` (нестандартный порт, чтобы не конфликтовать с локальным Postgres на 5432). Миграции применяются автоматически при запуске сервиса (см. `RUN_MIGRATIONS_ON_START`).

> **Если корневая директория содержит не-ASCII символы** (например, кириллицу), `docker compose` может не вывести дефолтное имя проекта. Передавайте его явно: `docker compose -p orgstructure up --build`.

Проверка работоспособности:

```bash
curl http://localhost:8080/healthz
# {"status":"ok"}
```

Остановка:

```bash
docker compose down
# полностью удалить данные:
docker compose down -v
```

### Локальный запуск без Docker

```bash
# Поднять только Postgres (на хосте 55432)
docker compose up -d postgres

# Запустить API локально, указав хост-порт Postgres
DB_PORT=55432 go run ./cmd/api
```

Переменные окружения см. в [.env.example](.env.example).

## Структура проекта

```
.
├── cmd/api/                  # точка входа: загрузка конфига, БД, миграции, HTTP-сервер
├── internal/
│   ├── app/                  # инициализация (logger, DB, goose)
│   ├── config/               # чтение конфигурации из env
│   ├── domain/               # модели + DTO (GORM-теги, JSON-теги, Nullable[T])
│   ├── errs/                 # типизированные ошибки + маппинг в HTTP-статусы
│   ├── repository/           # GORM-доступ (CTE для дерева, цикл-чек, name-uniqueness)
│   ├── service/              # бизнес-логика (валидация, cascade/reassign, cycle prevention)
│   ├── transport/http/       # HTTP-обработчики, роутер, middleware
│   └── validator/            # хелперы валидации полей
├── migrations/               # SQL-миграции goose
├── Dockerfile
├── docker-compose.yml
├── Makefile
└── README.md
```

Архитектура — классическая чистая трёхслойка: **transport → service → repository**. Слои общаются через интерфейсы (`service.DepartmentRepo`, `service.EmployeeRepo`), что позволяет писать unit-тесты с in-memory подменой репозиториев и легко менять реализацию хранилища.

## API

Все эндпоинты возвращают `application/json`. Ошибки имеют единый формат:

```json
{
  "error": "department name \"Backend\" already exists in this parent: conflict",
  "code": "conflict"
}
```

Ошибки валидации добавляют список полей:

```json
{
  "error": "validation failed",
  "code": "validation_error",
  "details": [
    {"field": "name", "message": "must not be empty"}
  ]
}
```

### Подразделения

#### POST `/departments/` — создать подразделение

```http
POST /departments/
Content-Type: application/json

{ "name": "Engineering", "parent_id": null }
```

- `name` — строка 1..200, обрезаются пробелы по краям, в пределах одного `parent` должно быть уникально (409 при дубле).
- `parent_id` — id существующего подразделения или `null` (root). Если родителя нет — 404.

Ответ: `201 Created` + созданное подразделение.

#### GET `/departments/{id}` — детали + поддерево

Параметры запроса:

| Параметр             | Тип    | Default      | Описание                                                  |
|----------------------|--------|--------------|-----------------------------------------------------------|
| `depth`              | int    | `1`          | Глубина поддерева в ответе (0..5; жёсткий cap = 5)        |
| `include_employees`  | bool   | `true`       | Включать ли сотрудников в каждый узел                     |
| `sort_employees_by`  | string | `created_at` | Сортировка сотрудников: `created_at` или `full_name`      |

Ответ — рекурсивная структура:

```json
{
  "department": {"id": 1, "name": "Root", "parent_id": null, "created_at": "..."},
  "employees": [
    {"id": 5, "department_id": 1, "full_name": "Alice", "position": "Lead", "hired_at": "2024-01-15", "created_at": "..."}
  ],
  "children": [
    {
      "department": {...},
      "employees": [...],
      "children": []
    }
  ]
}
```

#### PATCH `/departments/{id}` — переименовать / переместить

```http
PATCH /departments/{id}
Content-Type: application/json

{ "name": "Backend", "parent_id": 4 }
```

- Оба поля опциональны. `parent_id: null` — поднять подразделение в корень.
- Нельзя сделать подразделение его собственным предком (цикл) → 409.
- Нельзя дать имя, уже занятое другим подразделением у этого же родителя → 409.

#### DELETE `/departments/{id}` — удалить

Параметры запроса:

| Параметр                       | Значения                | Описание |
|--------------------------------|--------------------------|----------|
| `mode`                         | `cascade` \| `reassign`  | Обязателен |
| `reassign_to_department_id`    | int                      | Обязателен при `mode=reassign` |

- `cascade` — удаляет подразделение, всех его сотрудников и **всё поддерево** дочерних подразделений (через `ON DELETE CASCADE` в схеме).
- `reassign` — перед удалением все сотрудники поддерева перемещаются в `reassign_to_department_id`, после чего поддерево удаляется (всё в одной транзакции). Целевое подразделение не должно находиться внутри удаляемого поддерева (иначе 409).

Ответ: `204 No Content`.

### Сотрудники

#### POST `/departments/{id}/employees/` — создать сотрудника

```http
POST /departments/1/employees/
Content-Type: application/json

{ "full_name": "Alice", "position": "Senior Engineer", "hired_at": "2024-01-15" }
```

- Подразделение должно существовать (иначе 404).
- `full_name`, `position` — 1..200 после трима пробелов.
- `hired_at` — `YYYY-MM-DD` или отсутствует.

Ответ: `201 Created` + созданный сотрудник.

## Бизнес-правила

| Правило                                                  | Код ответа         |
|----------------------------------------------------------|--------------------|
| Сотрудник в несуществующем подразделении                 | 404 Not Found       |
| Пустое / слишком длинное `name`/`full_name`/`position`   | 422 Unprocessable   |
| Дубль `name` в пределах одного `parent_id`               | 409 Conflict        |
| Подразделение становится своим собственным предком       | 409 Conflict        |
| `mode` не передан / неизвестен                           | 400 Bad Request     |
| `reassign_to_department_id` отсутствует или внутри поддерева | 400 / 409       |

## Тесты

```bash
go test ./...
# или подробно:
go test -v -count=1 ./...
```

В проекте:

- **Unit-тесты сервисов** ([internal/service/department_test.go](internal/service/department_test.go), [internal/service/employee_test.go](internal/service/employee_test.go)) — проверяют валидацию, cascade/reassign delete, цикл-prevention, дубли имени в parent.
- **HTTP-тесты** ([internal/transport/http/router_test.go](internal/transport/http/router_test.go)) — поднимают сервер через `httptest.NewServer` с in-memory фейковыми репозиториями и проверяют коды ответов, JSON-формат, маршрутизацию.

Тесты не требуют запущенного Postgres — фейковые реализации `service.DepartmentRepo` / `service.EmployeeRepo` живут только в коде тестов.

## Логирование

`log/slog`, по умолчанию JSON. Каждый HTTP-запрос пишет одну строку:

```json
{"time":"...","level":"INFO","msg":"http request","method":"POST","path":"/departments/","status":201,"bytes":86,"duration_ms":2}
```

Уровень и формат задаются переменными `LOG_LEVEL` (`debug|info|warn|error`) и `LOG_FORMAT` (`text|json`).

## Миграции

Файлы в `migrations/`. Запускаются автоматически при старте сервиса (`RUN_MIGRATIONS_ON_START=true`). Вручную через goose CLI (если установлен):

```bash
make migrate-status
make migrate-up
make migrate-down
```
