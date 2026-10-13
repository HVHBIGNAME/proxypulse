# ProxyPulse

**Быстрый поиск и отбор рабочих прокси в пару кликов.**

ProxyPulse скачивает публичные пулы прокси, параллельно проверяет каждую
кандидатуру, отбрасывает мёртвые, измеряет пинг/скорость/анонимность и
показывает лидерборд. Лучший прокси — в буфере обмена одним нажатием.

```
НАЙТИ ПРОКСИ  →  таблица рабочих прокси, отсортированных по пингу  →  КОПИРОВАТЬ ТОП
```

## Что делает

- **Собирает** ~12 000 уникальных адресов из 23 пулов (HTTP и SOCKS5) параллельно, перемешивая их round-robin.
- **Замеряет** пинг, скорость скачивания (КБ/с) и класс анонимности (elite/anonymous/transparent).
- **Сортирует** по пингу и держит свежий результат; мёртвые не показываются.
- **Отдаёт результат**: клик — прокси в буфере обмена; кнопка сохранения — список `.txt` или `.json`.
- **Перепроверяет** выбранный прокси по запросу (пинг + актуальность).
- **Кэширует** результаты в SQLite, показывая их сразу при следующем запуске.

## Установка

Нужен только Go 1.21+ (для сборки из исходников).

```bash
git clone https://github.com/HVHBIGNAME/proxypulse
cd proxypulse

# CLI-версия
go build -o proxypulse.exe ./cmd/proxypulse

# GUI-версия (нативное окно Windows)
./scripts/build-gui.sh
```

Готовый `proxypulse-gui.exe` не требует ничего, кроме Windows 10/11 x64.

## Использование

### Графическое приложение (два клика)

1. Запустите `proxypulse-gui.exe`.
2. Нажмите **НАЙТИ ПРОКСИ** — пойдёт сбор и проверка.
3. Дождитесь строки «Готово» и нажмите **КОПИРОВАТЬ ТОП** — рабочий прокси в буфере.
4. Вставляйте куда нужно (браузер, скрипт, клиент): `http://IP:PORT` или `socks5://IP:PORT`.

Двойной клик по строке копирует именно её. Кнопка **ПРОВЕРИТЬ ВЫБРАННЫЙ**
перепроверяет выбранную строку на актуальность.

### Командная строка

```bash
# Полный проход: собрать, проверить, показать топ-15, сохранить всё в файл
proxypulse.exe -limit 2000 -top 15 -out proxies.txt

# Только быстрые (>= 500 КБ/с), без проверки анонимности
proxypulse.exe -min-speed 500 -anon=false

# Список доступных пулов
proxypulse.exe -list-sources

# Перепроверить только то, что уже есть в кэше
proxypulse.exe -db-only -top 20
```

Ключевые флаги:

| Флаг | По умолчанию | Значение |
|------|--------------|----------|
| `-limit` | 0 (все) | сколько кандидатов проверять |
| `-workers` | 300 | одновременных проверок |
| `-timeout` | 4s | таймаут на прокси |
| `-top` | 10 | сколько строк показать |
| `-min-speed` | 0 | отсечь медленные (КБ/с) |
| `-speed` / `-anon` | true | замер скорости / анонимности |
| `-out` / `-json` | — | файл со списком / JSON |
| `-best` | — | вывести ТОЛЬКО один лучший прокси и выйти |
| `-format` | text | с `-best`: `text` или `json` |
| `-list-sources` | — | показать пулы и выйти |

### Для скриптов и агентов

`-best` печатает ровно одну строку — готовый прокси — и ничего больше.
Прогресс уходит в stderr, так что stdout чист для захвата:

```bash
proxy=$(proxypulse -best -limit 800)          # http://IP:PORT или socks5://IP:PORT
curl -x "$proxy" https://api.ipify.org/       # сразу использовать
proxypulse -best -format json | jq -r .addr   # метаданные (пинг, скорость, анонимность)
proxypulse -db-only -best                      # взять лучший из кэша без сканирования
```

Результаты по умолчанию: `%LOCALAPPDATA%\ProxyPulse\` (`proxies.json`, `best.txt`).

## Источники

Официальные публичные списки прокси, только чтение:

| ID | Пул |
|----|-----|
| `monosans-http`, `monosans-socks5` | monosans/proxy-list |
| `thespeedx-http`, `thespeedx-socks5` | TheSpeedX/PROXY-List |
| `roosterkid-https`, `roosterkid-socks5` | roosterkid/openproxylist |
| `proxifly-http` | proxifly/free-proxy-list |
| `proxyscrape-http`, `proxyscrape-socks5` | ProxyScrape API |
| `geonode` | geonode.com API |
| `vakhov` | vakhov/fresh-proxy-list (JSON, per-protocol) |
| `murong-http`, `murong-socks5` | MuRongPIG/Proxy-Master |
| `hideipme-http`, `hideipme-socks5` | zloi-user/hideip.me (с гео) |
| `hookzof-socks5` | hookzof/socks5_list |
| `shifty-http`, `shifty-socks5` | ShiftyTR/Proxy-List |
| `clarketm` | clarketm/proxy-list |
| `aliilapro` | ALIILAPRO/Proxy |
| `anonwork-http` | anonym0uswork1221/free-proxies |
| `prxchk-http`, `prxchk-socks5` | prxchk/proxy-list |

Всего **23 пула**, ~12 000 уникальных адресов после дедупликации. Кандидаты
перемешиваются **round-robin** между пулами, а каждый пул отдаёт не больше
`DefaultCapPerSource` (1200) записей, равномерно распределённых по списку, —
поэтому гигантские зеркала (100k+ адресов) не вытесняют остальные пулы.

## Как это устроено

```
cmd/proxypulse        CLI
cmd/proxypulse-gui     Windows GUI (lxn/walk)
internal/sources       загрузка и разбор пулов
internal/proxy         проверка за прокси, пинг, скорость, анонимность
internal/app           конвейер: fetch → validate → enrich → sort
internal/store         JSON-кэш и экспорт
```

Вес и скорость: без внешних СУБД — только `lxn/walk` и `x/net`.
CLI ≈ 6 МБ, GUI ≈ 10 МБ, старт мгновенный, кэш читается одним `ReadFile`.

Проверка живого прокси — реальный `GET https://api.ipify.org/` через прокси:
успех = ответ 200 и полученный адрес выхода. Анонимность определяется по
`httpbin.org/headers` (утечка реального IP = transparent).

## Ограничения

- Публичные бесплатные прокси **нестабильны**: их средний срок жизни — часы.
  Всегда перепроверяйте выбранный адрес перед использованием (`ПРОВЕРИТЬ ВЫБРАННЫЙ`).
- Бесплатный прокси не даёт гарантий приватности; не используйте для
  чувствительных данных.
- Инструмент только читает публичные списки и проверяет доступность. Он не
  обходит платные квоты и не создаёт аккаунты.

## Лицензия

MIT — см. [LICENSE](LICENSE).