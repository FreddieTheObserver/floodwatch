# floodwatch

A Telegram bot that warns people in and around Bangkok when canals near them approach their banks, water is rising fast, or rain is heavy.
It was started on 26 September 2026, the day Bangkok declared all 50 districts flood disaster areas.

> floodwatch is unofficial.
> It relays public sensor data and can be late, wrong or silent.
> For official information call the BMA hotline 1555, or 1669 for emergencies.

## How it works

A collector polls public water level and rain gauge feeds every 10 minutes and stores each reading in Postgres.
After each poll, an evaluator judges every subscribed place against the stations around it and works out what changed since its subscriber was last told.
Each changed place gets one Telegram message.

```
ThaiWater / BMA feeds --> collector --> Postgres --> evaluator --> Telegram
```

Readings are keyed by the source's own observation time, so polling the same reading twice stores it once and a gap in polling never invents data.
An alert is recorded as sent only after Telegram accepts the message, so a failed send is retried on the next evaluation rather than lost.

## Alert rules

| Rule | Judged per | Severity |
| --- | --- | --- |
| Water level | water gauge | watch within 50 cm of the bank, warning within 20 cm, overflow at or above it |
| Water rising | water gauge | already within 50 cm of the bank, rising at least 5 cm/h, and on course to reach it within 3 hours |
| Rain | place | worst nearby gauge: 20/40/60 mm in 1 h, 40/70/100 mm in 3 h, or 90/150/250 mm in 24 h |
| Data gone quiet | gauge or place | no fresh reading for 3 hours |

The 60 mm/h line is roughly the drainage capacity commonly cited for Bangkok.
The 90 mm/24 h line is where the Thai Meteorological Department's "very heavy rain" class begins.

Gates near the river mouth rise 20 to 30 cm/h on every flood tide while far below their banks, which is why a fast rise only counts once the water is already close.
Rain alerts name the window that set them, so a day's total is not mistaken for rain falling now.

A severity rises at once but only falls once the reading is clearly past the threshold (5 cm for water, 20% for rain), so a reading hovering on a line does not alert every poll.
A gauge with no fresh data never produces an "all clear"; the last thing its subscriber was told stands until real data says otherwise.

Each place watches every station inside its radius (5 km by default) and always at least its 2 nearest working water gauges and 3 nearest working rain gauges within 10 km.
Water gauges are sparse, so for many districts the nearest one is several kilometres away.

## Data sources

| Source | What | Status |
| --- | --- | --- |
| [ThaiWater](https://www.thaiwater.net/) (Hydro-Informatics Institute) | water level and rain gauges for Bangkok and the five surrounding provinces | in use |
| [BMA Drainage and Sewerage Department](https://weather.bangkok.go.th/) | 125 rain gauges across Bangkok | off until the department permits automated access; requested 26 September 2026 |

ThaiWater publishes rain hourly and about an hour late, so rain alerts can trail real rainfall by one to two hours.
The collector identifies itself in its User-Agent, polls sources one at a time, and backs off a failing source up to once an hour.

## Running it

Requirements: Go 1.26 and Docker.

Create a bot with [@BotFather](https://t.me/BotFather), then store its token in an untracked `.env` without it reaching the screen or your shell history:

```sh
read -rsp 'Token: ' t && printf 'FLOODWATCH_TELEGRAM_TOKEN=%s\n' "$t" > .env && chmod 600 .env && unset t
```

```sh
make db-up    # Postgres 18 on localhost:5433
make serve    # builds, applies migrations, then runs the collector and the bot
```

`make run` does the same through `go run`.
Both load `.env` if it exists.
Without a token the collector still runs, just without the bot.
A token Telegram rejects stops the whole process, since collecting without ever alerting would look healthy while helping nobody.

Configuration is read from the environment:

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLOODWATCH_DSN` | set by the Makefile for the compose database | Postgres connection string (required) |
| `FLOODWATCH_POLL_INTERVAL` | `10m` | time between polls; at least `5m` to go easy on the public servers |
| `FLOODWATCH_FETCH_TIMEOUT` | `30s` | limit for one source's request |
| `FLOODWATCH_PROVINCES` | `10,11,12,13,73,74` | ThaiWater province codes to collect |
| `FLOODWATCH_BMA_RAIN_ENABLED` | `false` | poll the BMA rain gauges |
| `FLOODWATCH_TELEGRAM_TOKEN` | none | bot token from @BotFather; keep it in `.env` |
| `FLOODWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

## Using the bot

Send the bot a location, or paste coordinates such as `13.6515, 100.4945` from a map app on a computer.
Your first place is called Home; later locations ask whether to move a place there or add a new one, up to five.
Each new place gets a status message straight away, and after that the bot only writes when something changes.

| Command | What it does |
| --- | --- |
| `/status` | current water levels and rain around each of your places |
| `/places` | list your places, with buttons to remove them |
| `/stop` | delete your places and stop all alerts, after a confirmation |
| `/help` | how the bot works |

The bot stores only your chat ID and the coordinates you send.
If you block it, it deletes them on its next attempt to message you.
It ignores group chats, where alerts would expose every member's places.

## Development

```sh
make check    # vet, staticcheck, sqlc drift check, and all tests
make sqlc     # regenerate internal/store/gen after changing queries
make psql     # shell into the compose database
```

Store tests start their own Postgres with testcontainers, so they need Docker but never touch the compose database.

| Package | Role |
| --- | --- |
| `internal/source` | fetch and normalise each feed; fixtures in `testdata` are trimmed real responses |
| `internal/collect` | poll loop, per-source isolation and backoff |
| `internal/alert` | pure rules and the evaluator |
| `internal/bot` | Telegram sign-up, commands, message rendering and alert delivery |
| `internal/telegram` | minimal Bot API client that keeps the token out of errors and logs |
| `internal/store` | Postgres access through sqlc, with embedded goose migrations |
| `internal/config` | environment configuration |
