# FocusFlow

FocusFlow — русскоязычная персональная PWA для задач, месячных целей, календарного планирования, учёта времени, заметок и интервальных повторений.

## Что уже работает

- аккаунты с Argon2id и серверными HttpOnly-сессиями;
- Inbox/Today/Upcoming, приоритеты, цели, подзадачи и RRULE-повторения;
- расчёт месячной нагрузки, проверка доступной ёмкости и создание непересекающихся фокус-блоков;
- календарь day/week/month/list, drag-and-drop, resize и ICS import/export;
- один устойчивый stopwatch/Pomodoro-таймер с сохранением фактического времени;
- in-app и Web Push напоминания с PostgreSQL worker;
- BlockNote-редактор, дерево страниц, автосохранение, поиск и вложения до 10 МБ;
- Basic/Cloze карточки, FSRS v6, четыре оценки и неизменяемая история повторений;
- локальная двусторонняя синхронизация содержимого через AnkiConnect;
- адаптивный desktop/mobile UI, светлая/тёмная тема и устанавливаемая PWA;
- аналитика фокуса, целей и удержания знаний.

Все постоянные данные находятся в PostgreSQL-схеме `focusflow`. IndexedDB и service worker используются только как временный кэш.

## Быстрый запуск

Нужны Go 1.26+, Node 24+, pnpm и PostgreSQL 17+.

1. Скопируйте `.env.example` в `.env`.
2. Запустите PostgreSQL:

   ```powershell
   docker compose up -d postgres
   ```

3. Запустите API:

   ```powershell
   cd services/api
   go run ./cmd/server
   ```

   При старте API автоматически применяет идемпотентную миграцию `00001_init.sql`.

4. В другом терминале запустите клиент:

   ```powershell
   pnpm install
   pnpm dev
   ```

5. Откройте `http://127.0.0.1:5173`. Можно зарегистрироваться или нажать «Открыть демо» в development-режиме.

Полная контейнерная сборка:

```powershell
docker compose up --build
```

После этого приложение доступно на `http://localhost:8080`.

## AnkiConnect

1. Установите add-on AnkiConnect и запустите Anki Desktop.
2. Разрешите origin приложения в `webCorsOriginList` AnkiConnect.
3. Откройте «Настройки → AnkiConnect», проверьте соединение, выберите колоду и запустите синхронизацию.

Синхронизируется содержимое и теги карточек. Расписание и review history Anki намеренно остаются независимыми от встроенного FSRS.

## Push

Для локальных уведомлений достаточно разрешения браузера. Для фоновой Web Push-доставки задайте `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` и `VAPID_SUBJECT`. В production необходим HTTPS.

## Проверки

```powershell
cd services/api
go test ./...

cd ../../
pnpm --dir apps/web typecheck
pnpm --dir apps/web test
pnpm --dir apps/web build
```

Контракт API находится в `services/api/openapi.yaml`. Архитектура использует один origin в production: Go раздаёт собранный React-клиент и `/api/v1`.

## GitHub CI/CD

Workflow `.github/workflows/pipeline.yml` запускается для pull request, push в `main`,
тегов `v*` и вручную через Actions:

- проверяет форматирование, `go vet`, Go-тесты с race detector и сборку API;
- устанавливает зависимости строго по `pnpm-lock.yaml`, выполняет typecheck, тесты и PWA-сборку;
- проверяет production Docker-образ;
- после успешных проверок `main` или тега публикует `linux/amd64` и `linux/arm64`
  образ в `ghcr.io/<owner>/<repository>`.

Для GHCR не нужны дополнительные секреты: workflow использует ограниченный
`GITHUB_TOKEN` с правом `packages: write`. Тег `latest` обновляется из `main`;
релиз `v1.2.3` создаёт контейнерные теги `1.2.3`, `1.2` и `sha-…`.

Рекомендуемые правила ветки `main` в GitHub:

1. Require a pull request before merging.
2. Require status checks: `Web`, `Go API`, `Docker`.
3. Require branches to be up to date before merging.
