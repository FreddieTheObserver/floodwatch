# Translating FloodWatch

FloodWatch speaks English and Thai.
Every word it sends lives in one catalogue per language: `internal/bot/en.go` and `internal/bot/th.go`.
A test fails if any phrase is missing from a language, so no reader can get a message that switches language halfway.

The Thai catalogue has had one round of native-speaker review, on 26 September 2026, which reworded the trends, rates, regional labels, source line and disclaimer.
Its safety wording still needs checking most: the risk names and the actions should use the words Thai residents already know from official flood warnings.
Two choices made while applying that review should also be confirmed: the rain counterpart ข้อมูลฝนในพื้นที่ of the reviewed ข้อมูลระดับน้ำในพื้นที่, and the reviewed สถานการณ์ prefix extended from แย่ลง to ดีขึ้น and ทรงตัว.
The messages owning up to time FloodWatch was not running were added on 27 September 2026 and have not been reviewed yet: `Offline`, `OfflinePlaces`, `OfflineStatus` and `LateReply`, with dates written like 25 ก.ย. 23:10 น.
Nor have the tide forecasts added the same day: `TideHigh`, `TideRising`, `TideFalling` and `TidePredictions`, which must read as expectations rather than measurements.

## How a person's language is chosen

A person gets Thai if their Telegram app is set to Thai, and English otherwise.
The `/language` command switches, and the choice is remembered so that alerts, which arrive with no app language to go by, use it too.
Station names come from ThaiWater, which publishes Thai names for its stations.

## Glossary for review

These terms appear throughout, so agree on them first.

| English | Draft Thai | Where it appears |
| --- | --- | --- |
| LOW | ปกติ | risk level |
| WATCH | เฝ้าระวัง | risk level |
| WARNING | เตือนภัย | risk level |
| HIGH | วิกฤต | risk level |
| NO DATA | ไม่มีข้อมูล | risk level |
| getting worse / steady / improving | สถานการณ์แย่ลง / สถานการณ์ทรงตัว / สถานการณ์ดีขึ้น | trend |
| around Home | บริเวณ บ้าน | headline |
| What to do | สิ่งที่ควรทำ | heading |
| bank (of a canal or river) | ตลิ่ง | water readings |
| 0.56 m above the bank | สูงกว่าตลิ่ง 0.56 ม. | water readings |
| rising 26 cm/h | เพิ่มขึ้น 26 ซม./ชม. | water readings |
| Regional: (a distant water gauge) | ข้อมูลระดับน้ำในพื้นที่: | what is happening |
| Regional: (a distant rain gauge) | ข้อมูลฝนในพื้นที่: | what is happening |
| regional (a station outside your radius) | นอกรัศมี | details |
| Wettest of 8 nearby gauges reporting | มีปริมาณฝนสูงสุดเมื่อเทียบกับ 8 สถานีใกล้เคียงที่ส่งข้อมูล | details |
| threshold | เกณฑ์ | rain readings |
| gauge / station | สถานีวัด | throughout |
| Measured 17:40 · 20 min ago | วัดเมื่อ 17:40 น. · 20 นาทีที่แล้ว | details |
| Flooded roads right now | ถนนที่มีน้ำท่วมตอนนี้ | map link |
| FloodWatch was offline from 23:10 to 07:45 | FloodWatch ไม่ได้ทำงานตั้งแต่ 23:10 น. ถึง 07:45 น. | offline notice |
| Sorry for the late reply | ขออภัยที่ตอบช้า | late reply |
| Tide forecast | คาดการณ์น้ำขึ้นน้ำลง | tide forecast |
| at high water around 18:10 | เมื่อน้ำขึ้นสูงสุดราว 18:10 น. | tide forecast |
| should be about 0.35 m below its bank | คาดว่าจะต่ำกว่าตลิ่งประมาณ 0.35 ม. | tide forecast |
| the tide is coming in / going out | น้ำกำลังขึ้น / น้ำกำลังลง | tide forecast |
| Tide predictions: HII | ข้อมูลน้ำขึ้นน้ำลง: สสน. | sources |
| Data: ThaiWater (HII) | แหล่งข้อมูล: ThaiWater (สสน.) | sources |
| This is not an official warning | ข้อมูลนี้ไม่ใช่ประกาศเตือนภัยอย่างเป็นทางการ | disclaimer |
| BMA hotline 1555, emergency 1669 | สายด่วน กทม. 1555 · เหตุฉุกเฉิน 1669 | disclaimer |

The actions for each risk level are in `Actions` in `th.go`, one short line each.

## What to check

1. Do the five risk names match the words official Thai flood warnings use for the same levels?
2. Are the actions clear, short and safe, and would anyone read them as an evacuation order where none is meant?
3. Does each sentence read naturally, rather than as a word-for-word translation?
4. Is the bot's voice right? The draft avoids pronouns such as ฉัน or ผม and names FloodWatch instead.

## Changing a phrase

Edit the phrase in `internal/bot/th.go`, then run `make check`.
Keep the `%s` and `%d` placeholders: each marks a value filled in at send time, such as a station name or a number.
Keep the ✏️ at the start of `RenamePrompt`, which is how the bot recognises replies to its naming question in any language.

## Adding a language

Copy `en.go`, translate every field, add the language to `languages` in `i18n.go`, to `languageFor`, and to the `language` check in the database migration, then run `make check`.
A missing phrase fails the completeness test.
