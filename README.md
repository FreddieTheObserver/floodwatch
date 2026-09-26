# floodwatch

A Telegram bot that warns people in and around Bangkok when canals near them approach their banks, water is rising fast, or rain is heavy.
It was started on 26 September 2026, the day Bangkok declared all 50 districts flood disaster areas.

> floodwatch is unofficial.
> It relays public sensor data and can be late, wrong or silent.
> For official information call the BMA hotline 1555, or 1669 for emergencies.

## How it works

A collector polls public water level and rain gauge feeds every 10 minutes and stores each reading in Postgres.
After each poll, an evaluator judges every subscribed place against the stations around it and turns the readings into one overall flood risk.
A subscriber is messaged only when that risk changes, and each message leads with the verdict, the reason for it and what to do, rather than numbers to interpret.

```
ThaiWater / BMA feeds --> collector --> Postgres --> evaluator --> Telegram
```

Readings are keyed by the source's own observation time, so polling the same reading twice stores it once and a gap in polling never invents data.
An alert is recorded as sent only after Telegram accepts the message, so a failed send is retried on the next evaluation rather than lost.

## Overall risk

| Risk | Meaning | What the bot advises |
| --- | --- | --- |
| 🟢 LOW | nothing nearby is at a warning level | nothing to do |
| 🟡 WATCH | something is raised | keep an eye on it and check the flooded roads map |
| 🟠 WARNING | water near the bank, rising fast, or very heavy rain | move the car to higher ground and valuables off the floor |
| 🔴 HIGH | water over the bank nearby, or rain beyond drainage capacity | stay off flooded roads and follow official BMA instructions |
| ⚪ NO DATA | no gauge nearby has fresh data | check official BMA updates |

The risk is the worst of the place's water gauges and its rain, judged by the rules below.
A gauge beyond the place's radius counts one level lower, since a canal overflowing 7 km away is a warning for the place, not a certainty; messages label it regional and give its distance.
Water rising fast near its bank counts one level higher, since it is about to get worse, and distance is discounted after that, so a distant rise cannot win back its level.
The trend reads getting worse if anything behind the risk is worsening, improving if everything behind it is easing, and steady otherwise.

When gauges go quiet, their last judged levels hold until fresh data replaces them, so an outage never produces an all clear.
The risk reads NO DATA only when nothing nearby is reporting and nothing was raised before.

## Gauge rules

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

A severity rises at once but only falls once the reading is clearly past the threshold (5 cm for water, 20% for rain), so a reading hovering on a line does not flip the risk every poll.

Each place watches every station inside its radius (5 km by default) and always at least its 2 nearest working water gauges and 3 nearest working rain gauges within 10 km.
Water gauges are sparse, so for many districts the nearest one is several kilometres away.

## Data sources

| Source | What | Status |
| --- | --- | --- |
| [ThaiWater](https://www.thaiwater.net/) (Hydro-Informatics Institute) | water level and rain gauges for Bangkok and the five surrounding provinces | in use |
| [BMA Drainage and Sewerage Department](https://weather.bangkok.go.th/) | about 120 rain gauges across Bangkok | used as republished by ThaiWater since 26 September 2026, credited to the department; polling its own site stays off until it permits automated access |
| [HII tide predictions](https://www.thaiwater.net/water/ocean) | hourly predicted tide, a year ahead, for the Chao Phraya from Bangkok to its mouth and the Tha Chin mouth | collected every 6 hours since 26 September 2026; tide forecasts in messages since 27 September 2026, setting no risk yet |

HII's files cover a whole year and are revised, so each sync stores only the three days behind and fourteen days ahead of now.

## Tide forecasts

Gauges near the river mouths rise and fall with the tide, by a metre or more a day.
After every poll, FloodWatch fits each water gauge's last 3 days of readings to the tide station and delay that best explain them, as a straight line from the predicted tide to the gauge's level.
A gauge counts as tidal while that fit explains its readings with a correlation of at least 0.8; on 27 September 2026 that was 5 of the 22 gauges with enough history, all near the Chao Phraya and Tha Chin mouths, while the canal gauges inland follow their pumps and gates instead.

For a tidal gauge, the highest level of the next 6 hours is forecast from the predicted tide, shifted by how far the gauge's last hour of readings sits above or below its fit.
That shift is the water the tide does not explain, such as flood water coming down the river, and it is assumed to hold over the 6 hours.
Replaying the day before, with each fit made only from the readings before it, the highest level was forecast within 8 cm on average and within 18 cm 9 times in 10, where assuming no change missed by 45 cm on average.

Messages state the forecast under what is happening, rounded to 5 cm and worded as an expectation, for the tidal gauges behind the risk or else the nearest one watched.
Forecasts do not yet set any risk.
Each one is recorded against the reading it was made from, so that after a week or so they can be checked against what the gauges went on to do before they are allowed to.

The regular poll sees only each gauge's latest reading, so every 6 hours the collector also fetches the last 3 days of readings of each water gauge that is still reporting, from the series behind ThaiWater's website graphs.
That fills in whatever happened while FloodWatch was not running, and gives the comparison with the tide days of unbroken readings.
It asks for one gauge at a time with a pause between, keeps readings it already has, and stops after three failures in a row.

ThaiWater publishes rain hourly and about an hour late, so rain alerts can trail real rainfall by one to two hours.
Every gauge records the agency that runs it, and any message drawing on BMA gauges credits the department.
The collector identifies itself in its User-Agent, polls sources one at a time, and backs off a failing source up to once an hour.

## Running it

Requirements: Go 1.26 and Docker.

Create a bot with [@BotFather](https://t.me/BotFather), then store its token in an untracked `.env` without it reaching the screen or your shell history:

```sh
read -rsp 'Token: ' t && printf 'FLOODWATCH_TELEGRAM_TOKEN=%s\n' "$t" > .env && chmod 600 .env && unset t
```

```sh
make db-up    # Postgres 18 on localhost:5433, restarted by Docker if it stops
make up       # the service, detached in tmux and restarted whenever it exits
make down     # stops it
```

`make up` appends the service's output to `floodwatch.log`; `tmux attach -t floodwatch` shows it live.
`make serve` runs the service once in the foreground, and `make run` does the same through `go run`.
All of them load `.env` if it exists.
Without a token the collector still runs, just without the bot.
A token Telegram rejects stops the whole process, since collecting without ever alerting would look healthy while helping nobody.

## Watchdog

A process that has died cannot say so, and a silent bot looks exactly like a quiet day.
So floodwatch reports to an outside watchdog, [healthchecks.io](https://healthchecks.io), which alarms when the reports turn to failures or stop.

After every poll it sends a success ping if data arrived, alerts could be worked out and delivered, and Telegram is answering.
After three unhealthy polls in a row, 30 minutes, it sends a failure ping listing what is wrong; a single bad poll is usually a blip the next one fixes.
If the process dies or the machine loses power or its connection, the pings stop and the watchdog alarms by itself.

Create a check with a 10 minute period and a 20 minute grace time, then store its ping URL beside the token, which anyone holding it could use to forge pings:

```sh
read -rsp 'Ping URL: ' u && printf 'FLOODWATCH_HEALTHCHECK_URL=%s\n' "$u" >> .env && unset u
```

Configuration is read from the environment:

| Variable | Default | Meaning |
| --- | --- | --- |
| `FLOODWATCH_DSN` | set by the Makefile for the compose database | Postgres connection string (required) |
| `FLOODWATCH_POLL_INTERVAL` | `10m` | time between polls; at least `5m` to go easy on the public servers |
| `FLOODWATCH_FETCH_TIMEOUT` | `30s` | limit for one source's request |
| `FLOODWATCH_PROVINCES` | `10,11,12,13,73,74` | ThaiWater province codes to collect |
| `FLOODWATCH_BMA_RAIN_ENABLED` | `false` | poll the BMA rain gauges |
| `FLOODWATCH_TELEGRAM_TOKEN` | none | bot token from @BotFather; keep it in `.env` |
| `FLOODWATCH_HEALTHCHECK_URL` | none | watchdog ping URL; keep it in `.env` |
| `FLOODWATCH_TIDE_STATIONS` | `N01,N02,N03,N04,N05` | HII tide stations to collect |
| `FLOODWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |

## Using the bot

Send the bot a location, or paste coordinates such as `13.6515, 100.4945` from a map app on a computer.
Your first place is called Home; later locations ask whether to move a place there or add a new one, up to five.
A new place starts as Place 2, Place 3 and so on, and the bot asks straight away what to call it; any place can be renamed later from `/places`.
Each new place gets a status message straight away, and after that the bot only writes when the place's overall risk changes.
Every status and alert has the same shape, whatever the risk: the risk and its trend, what is happening, a short list of what to do, the flooded roads map, then a collapsed Details section, and finally the check time and disclaimer.
What is happening states measurements against FloodWatch's thresholds, for example 96.5 mm over the last 24 hours against the 90 mm line, instead of labelling them.
Details hold only measurements, each with how old it is, plus what moved the trend and who runs each gauge; the verdict appears only above them.
Its Show gauges on map button sends the place and its nearest gauges as map pins that open in the phone's own maps app.
Status messages, and alerts about a raised risk, link to the BMA's [flooded roads map](https://now.bangkok.go.th/flood-alert.html), since a canal gauge kilometres away cannot say whether your street is under water.

A quiet bot must never pass for a quiet day.
So when alerts have gone unchecked or undelivered for an hour or more (three polls, if polling is slower), for example because the computer running FloodWatch slept, lost its connection or restarted, the bot owns up once it is running again.
Everyone with a place is told when it went offline and when it came back, with each place's current risk, before any alert about a risk that changed meanwhile.
Messages sent to the bot while it was offline, which Telegram holds for up to a day, are answered with an apology for the late reply.

| Command | What it does |
| --- | --- |
| `/status` | the flood risk around each of your places, why, and what to do |
| `/places` | list your places, with buttons to rename or remove them |
| `/language` | switch between Thai and English |
| `/stop` | delete your places and stop all alerts, after a confirmation |
| `/help` | how the bot works |

The bot speaks Thai to people whose Telegram app is set to Thai and English to everyone else, and remembers a choice made with `/language` so alerts use it too.
Thai readers see stations under their Thai names.
The Thai wording is a draft awaiting review; [docs/TRANSLATION.md](docs/TRANSLATION.md) has the glossary and how to change it.

The bot stores only your chat ID, your language and the coordinates you send.
If you block it, it deletes them on its next attempt to message you.
It ignores group chats, where alerts would expose every member's places.

## Development

```sh
make check    # gofmt, vet, staticcheck, sqlc drift check, and all tests
make sqlc     # regenerate internal/store/gen after changing queries
make psql     # shell into the compose database
```

Store tests start their own Postgres with testcontainers, so they need Docker but never touch the compose database.

| Package | Role |
| --- | --- |
| `internal/source` | fetch and normalise each feed; fixtures in `testdata` are trimmed real responses |
| `internal/collect` | poll loop, per-source isolation and backoff, the tide sync, and the gauge history fill |
| `internal/alert` | pure rules and the evaluator |
| `internal/tide` | fits gauges to the tide and forecasts their highest level |
| `internal/bot` | Telegram sign-up, commands, message rendering and alert delivery |
| `internal/telegram` | minimal Bot API client that keeps the token out of errors and logs |
| `internal/health` | reports each poll to the outside watchdog |
| `internal/store` | Postgres access through sqlc, with embedded goose migrations; also records when alerts were last delivered |
| `internal/config` | environment configuration |
