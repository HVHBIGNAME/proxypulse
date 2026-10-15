# ProxyPulse

**Рабочий прокси. В один клик.**

[Скачать для Windows](https://github.com/HVHBIGNAME/proxypulse/releases/latest) ·
[Что нового](CHANGELOG.md)

ProxyPulse скачивает публичные пулы прокси, параллельно проверяет каждую
кандидатуру, отбрасывает мёртвые, измеряет пинг/скорость/анонимность и
показывает лидерборд. Лучший прокси — в буфере обмена одним нажатием.

Полностью тёмный нативный интерфейс: графитовые панели, бирюзовый пульс,
тёмная таблица, поля, кнопки и раскрывающиеся списки. Поиск, фильтры и экспорт
доступны прямо в рабочем окне.

![Интерфейс ProxyPulse](assets/screenshot.png)

*На снимке — демонстрационные адреса из диапазонов, зарезервированных для документации.*

## Что делает

- **Собирает** кандидатов из 57 публичных лент HTTP/SOCKS5 параллельно, удаляет дубликаты и перемешивает источники round-robin.
- **Замеряет** пинг, скорость скачивания (КБ/с) и класс анонимности (elite/anonymous/transparent).
- **Сортирует** по пингу и держит свежий результат; мёртвые не показываются.
- **Отдаёт результат**: двойной клик — прокси в буфере; экспорт — список `.txt` или `.json`.
- **Перепроверяет** выбранный прокси по запросу (пинг + актуальность).
- **Кэширует** результаты в компактном JSON, показывая их сразу при следующем запуске
  с пометкой о необходимости перепроверки.
- **Фильтрует** по протоколу, пингу и анонимности; ищет по адресу, стране, источнику и выходному IP.
- **Помогает настроить проверку**: быстрый, сбалансированный, глубокий и пользовательский профили.

## Установка

Скачайте **`ProxyPulse-1.1.0-windows-x64.zip`** из
[релиза](https://github.com/HVHBIGNAME/proxypulse/releases/latest), распакуйте
и откройте `proxypulse-gui.exe`. В ZIP также входит CLI `proxypulse.exe`.
Можно скачать GUI отдельным EXE. Установка не нужна; Windows 10/11 x64.

### Сборка из исходников

Нужен **Go 1.26.2+**. Для GUI дополнительно нужны **Python 3.11+**, Pillow и
`windres` из MinGW-w64 в `PATH`. Python и MinGW нужны только для сборки.

```bash
git clone https://github.com/HVHBIGNAME/proxypulse
cd proxypulse

# CLI-версия
go build -trimpath -ldflags "-s -w" -o proxypulse.exe ./cmd/proxypulse

# GUI + CLI + ZIP + SHA-256 (из PowerShell, cmd или Bash)
python -m pip install pillow==12.2.0
python scripts/build_release.py --version 1.1.0
```

Артефакты появятся в `dist/`. Например, для MinGW в `C:\mingw64` добавьте
`C:\mingw64\bin` в `PATH`. В Git Bash также доступен `./scripts/build-gui.sh`.
Манифест Common Controls и иконка автоматически встраиваются в EXE.

Проверки разработчика:

```powershell
go vet ./...
go test -race ./...
# После сборки ресурсов; открывает тестовое окно, использует локальные данные.
$env:PROXYPULSE_GUI_SMOKE = '1'
go test ./cmd/proxypulse-gui -run TestNativeDesktop -count=1 -timeout 60s
```

На Linux проверяйте переносимые пакеты:
`go test -race ./internal/... ./cmd/proxypulse`.

## Использование

### Графическое приложение (два клика)

1. Запустите `proxypulse-gui.exe`.
2. Выберите профиль и нажмите **Найти прокси** — пойдёт сбор и проверка.
3. Дождитесь строки «Готово» и нажмите **Копировать лучший** — рабочий прокси в буфере.
4. Вставляйте куда нужно (браузер, скрипт, клиент): `http://IP:PORT` или `socks5://IP:PORT`.

Двойной клик по строке копирует именно её. Кнопка **Перепроверить** проверяет
выбранную строку на актуальность. **Копировать лучший** всегда использует самый
быстрый результат из всего списка, независимо от фильтров.

| Профиль | Кандидатов | Скорость / анонимность |
|---------|------------|-----------------------|
| Быстрый | 1 500 | выключены |
| Баланс | 10 000 | включены |
| Глубокий | 50 000 | включены |
| Весь пул | все, без выборки источников | включены |
| Свой | 0–200 000; 0 — все | настраиваются |

В обычных профилях каждая лента может дать до **10 000** уникальных кандидатов
вместо прежних 1 200. **«Весь пул»** или значение **0** снимает и этот лимит,
и ограничение числа проверок. Большие проходы занимают больше времени;
параллелизм проверок остаётся ограниченным, остановка доступна в любой момент.
В карточке охвата отдельно показываются собранный пул и прогресс проверки.

В GUI скорость измеряется для 60 лучших, анонимность — для 150 лучших.
Прочерк означает отсутствие успешного замера, а не нулевую скорость.
Кнопка остановки сохраняет уже полученные результаты. При ошибке загрузки
предыдущий список остаётся доступен.

**Клавиши:** `F5` — поиск, `Esc` — остановка, `Ctrl+F` — поиск по результатам,
`Ctrl+C` в таблице — выбранный прокси. Сортировка — по заголовкам колонок.
Можно экспортировать только видимые строки или весь список в TXT/JSON.
Анимации учитывают системную настройку Windows и не работают в простое.

### Командная строка

```bash
# Полный проход: собрать, проверить, показать топ-15, сохранить всё в файл
proxypulse.exe -limit 2000 -top 15 -out proxies.txt

# Только быстрые (>= 500 КБ/с), без проверки анонимности
proxypulse.exe -min-speed 500 -anon=false

# Список доступных пулов
proxypulse.exe -list-sources

# Перепроверить только то, что уже есть в кэше (с сетевой проверкой)
proxypulse.exe -db-only -top 20

# Версия сборки
proxypulse.exe -version

# Массовый сбор и проверка всех списков, без ограничения на источник
proxypulse.exe -source-cap 0 -limit 0 -workers 300 -out proxies.txt -json proxies.json

# Проверить до 50 000 кандидатов, до 20 000 из каждого источника
proxypulse.exe -source-cap 20000 -limit 50000 -out proxies.txt
```

Ключевые флаги:

| Флаг | По умолчанию | Значение |
|------|--------------|----------|
| `-limit` | 0 (все) | сколько кандидатов проверять |
| `-source-cap` | 10000 | кандидатов от одной ленты; 0 — вся лента |
| `-workers` | 300 | одновременных проверок |
| `-timeout` | 4s | таймаут на прокси |
| `-top` | 10 | сколько строк показать |
| `-min-speed` | 0 | отсечь медленные (КБ/с) |
| `-speed` / `-anon` | true | замер скорости / анонимности |
| `-out` / `-json` | — | файл со списком / JSON |
| `-best` | — | вывести ТОЛЬКО один лучший прокси и выйти |
| `-format` | text | с `-best`: `text` или `json` |
| `-list-sources` | — | показать пулы и выйти |
| `-db-only` | false | перепроверить рабочие записи кэша, без загрузки пулов |
| `-version` | — | показать версию и выйти |

### Для скриптов и агентов

`-best` печатает ровно одну строку — готовый прокси — и ничего больше.
В этом режиме прогресс отключён, ошибки идут в stderr — stdout чист для захвата:

```bash
proxy=$(proxypulse -best -limit 800)          # http://IP:PORT или socks5://IP:PORT
curl -x "$proxy" https://api.ipify.org/       # сразу использовать
proxypulse -best -format json | jq -r .addr   # метаданные (пинг, скорость, анонимность)
proxypulse -db-only -best                      # перепроверить кэш и выбрать лучший
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
| `geonode`, `geonode-2`…`geonode-5` | пять страниц geonode.com API |
| `vakhov` | vakhov/fresh-proxy-list (JSON, per-protocol) |
| `murong-http`, `murong-socks5` | MuRongPIG/Proxy-Master |
| `hideipme-http`, `hideipme-socks5` | zloi-user/hideip.me (с гео) |
| `hookzof-socks5` | hookzof/socks5_list |
| `shifty-http`, `shifty-socks5` | ShiftyTR/Proxy-List |
| `clarketm` | clarketm/proxy-list |
| `aliilapro` | ALIILAPRO/Proxy |
| `anonwork-http` | anonym0uswork1221/free-proxies |
| `prxchk-http`, `prxchk-socks5` | prxchk/proxy-list |
| `iplocate-http`, `iplocate-https`, `iplocate-socks5` | iplocate/free-proxy-list |
| `ercin-http`, `ercin-https`, `ercin-socks5` | ErcinDedeoglu/proxies |
| `zaeem-http`, `zaeem-https`, `zaeem-socks5` | Zaeem20/FREE_PROXIES_LIST |
| `mmpx-http`, `mmpx-https`, `mmpx-socks5` | mmpx12/proxy-list |
| `jetkai-http`, `jetkai-https`, `jetkai-socks5` | jetkai/proxy-list, online-proxies |
| `kang-http`, `kang-https`, `kang-socks5` | officialputuid/KangProxy |
| `vpslab-http`, `vpslab-socks5` | VPSLabCloud/VPSLab-Free-Proxy-List |
| `thordata-http`, `thordata-https`, `thordata-socks5` | Thordata/awesome-free-proxy-list |
| `proxyscrape-mirror-http`, `proxyscrape-mirror-https`, `proxyscrape-mirror-socks5` | GitHub-зеркало ProxyScrape |
| `r00tee-https`, `r00tee-socks5` | r00tee/Proxy-List |
| `dpangestuw-http`, `dpangestuw-socks5` | dpangestuw/Free-Proxy |

Всего **57 лент**. Кандидаты перемешиваются **round-robin** между источниками,
дубликаты удаляются при объединении, без промежуточного общего списка копий.
При ограниченном сборе выборка равномерно распределяется по каждой ленте.
Proxifly поддерживается с актуальным объектом геолокации и со старым строковым
форматом; HTTP и SOCKS5 не теряются из-за изменения схемы.

В контрольном запуске 15 октября 2026 года собрано **80 650 уникальных кандидатов**
при `-source-cap 10000`; из первых 10 000 проверку прошли **234**. Это результат
конкретного запуска, а не фиксированное число доступных или рабочих прокси.

## Как это устроено

```
cmd/proxypulse        CLI
cmd/proxypulse-gui     Windows GUI (lxn/walk)
internal/sources       загрузка и разбор пулов
internal/proxy         проверка за прокси, пинг, скорость, анонимность
internal/app           конвейер: fetch → validate → enrich → sort
internal/store         JSON-кэш и экспорт
```

Вес Windows x64 release-сборки: **GUI ≈ 10,29 МиБ**, **CLI ≈ 6,22 МиБ**,
**ZIP с обоими приложениями ≈ 6,53 МиБ**. Размер может немного отличаться между
версиями Go. Сборка с `-trimpath -s -w`, без CGO и внешних сред выполнения.
Контрольные суммы — в `SHA256SUMS.txt` рядом с загрузками.

Прогресс объединяется перед передачей UI, сетевой параллелизм ограничен,
а отмена останавливает постановку новых задач. Неудачная перепроверка удаляет
прокси из рабочих записей кэша; отмена запроса сама по себе этого не делает.

Проверка живого прокси — реальный `GET https://api.ipify.org/` через прокси:
успех = проверенный TLS-сертификат, ответ 200 и корректный IP выхода. Анонимность определяется по
`httpbin.org/headers` (утечка реального IP = transparent).

## Ограничения

- Публичные бесплатные прокси **нестабильны**: их средний срок жизни — часы.
  Всегда перепроверяйте выбранный адрес перед использованием (`Перепроверить`).
- Бесплатный прокси не даёт гарантий приватности; не используйте для
  чувствительных данных.
- Инструмент только читает публичные списки и проверяет доступность. Он не
  обходит платные квоты и не создаёт аккаунты.

## Лицензия

MIT — см. [LICENSE](LICENSE).
