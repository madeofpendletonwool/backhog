# Achievements catalogue

Reference for the achievement system: every catalogue entry, its tier, and
what unlocks it. The catalogue itself lives in code —
`api/internal/achievements/achievements.go` — and this file is
hand-maintained against it; `TestAchievementsDocSync` in that package fails
the build when the two drift apart.

## How it works

- **Domains** scope every entry to one arena: the games catalogue, the books
  catalogue (same spine, same rules, shelf-shaped numbers), or `any` — the
  eggs, which are about the app itself. A predicate only ever fires against
  its own domain's events; the store assembles each arena's aggregates
  separately, so a book finish never moves a game ladder.
- **Predicates** are pure functions over an event snapshot; the store
  evaluates them inside the mutation that triggered them and replays them
  over existing history on gallery loads (the backfill). Unlocks are
  idempotent (`INSERT OR IGNORE`).
- **Hidden** achievements mask their identity while locked: the card is
  served as `???` with teasing copy (`maskedHints`) and the lock icon. Only
  the tier leaks, so the gallery can still group them. The real identity
  reveals on unlock — in the toast and on the wall.
- **Eggs** (`egg` column) are easter eggs: hidden achievements with no
  predicate that unlock only by playing with the app. The client detects
  the interaction and fires `POST /api/achievements/{id}/egg`
  (auth-required, user-scoped). The endpoint accepts only ids on the egg
  whitelist, is idempotent, and is rate-limited to 10 requests per user per
  egg per minute. The reveal rides the normal unlock toast.
  - `night_owl` fires when a session is logged between 3:00 and 4:59 AM.
    Sessions store the day, not the hour, so the check is live against the
    user's own local clock at logging time — the only honest witness.
  - `hog_watcher` — ten clicks on the Backhog logo.
  - `konami` — the Konami code (↑↑↓↓←→←→BA) typed on the achievements page.
  - `queue_shuffler` — the same game moved to the top of the play queue
    five times in one sitting.

## Keeping this file honest

After changing the catalogue in Go, update the table below to match:
one row per entry, same columns, derived from the catalogue definition.
`go test ./internal/achievements/` enforces id, title, tier, hidden, egg,
and unlock text stay in sync.

## The catalogue

| ID | Title | Tier | Hidden | Egg | Unlock |
| --- | --- | --- | --- | --- | --- |
| `first_blood` | First Blood | bronze | false | false | Finish your first backlog game. |
| `cleanup_crew` | Cleanup Crew | silver | false | false | Finish 5 backlog games. |
| `spring_cleaning` | Spring Cleaning | silver | false | false | Finish 10 backlog games. |
| `deep_clean` | Deep Clean | gold | false | false | Finish 25 backlog games. |
| `hazmat_suit` | Hazmat Suit | gold | false | false | Finish 50 backlog games. |
| `the_purge` | The Purge | legendary | false | false | Finish 100 backlog games. |
| `one_down` | One Down | bronze | false | false | Finish a game while 10+ sit unplayed. |
| `next_up` | Next! | silver | false | false | Finish the game sitting at the top of your queue (marked finished straight from the queue). |
| `making_a_dent` | Making a Dent | silver | false | false | Shrink your unplayed backlog by 10 from its peak. |
| `dentist_appointment` | Dentist Appointment | gold | false | false | Shrink your unplayed backlog by 25 from its peak. |
| `mass_extinction` | Mass Extinction | legendary | false | false | Shrink your unplayed backlog by 50 from its peak. |
| `empty_the_closet` | Empty the Closet | gold | true | false | Reach zero unplayed games after hoarding 10+. |
| `backlog_negative` | Backlog Negative | gold | true | false | Finish more games in a year than you add. |
| `hat_trick` | Hat Trick | bronze | false | false | Finish 3 games in one calendar month. |
| `cleanup_month` | Cleanup Month | silver | false | false | Finish 5 games in one calendar month. |
| `backlog_machine` | Backlog Machine | gold | false | false | Finish at least one game in 3 consecutive months. |
| `perfect_season` | Perfect Season | legendary | true | false | Finish at least one game every month of a calendar year. |
| `season_opener` | Season Opener | bronze | false | false | Finish a backlog game in January. |
| `strong_finish` | Strong Finish | bronze | false | false | Finish a backlog game in December. |
| `summer_cleanup` | Summer Cleanup | silver | false | false | Finish 5 games in a single summer (June–August). |
| `restraint` | Restraint | silver | false | false | Go 30 days without adding a single game. |
| `discipline` | Discipline | gold | false | false | Go 90 days without adding a single game. |
| `dusty_relic` | Dusty Relic | bronze | false | false | Finish a game you've owned for 3+ years (owned = since you added it to Backhog). |
| `archaeologist` | Archaeologist | silver | false | false | Finish a game you owned for 5+ years. |
| `lost_civilization` | Lost Civilization | gold | false | false | Finish a game you've owned for 7+ years. |
| `ancient_artifact` | Ancient Artifact | legendary | false | false | Finish a game you've owned for 10+ years. |
| `time_capsule` | Time Capsule | silver | false | false | Finish a game released 10+ years before you finished it. |
| `old_hardware` | Old Hardware, New Victory | gold | false | false | Finish a game originally released before 2000. |
| `speedrun` | Speedrun | bronze | false | false | Finish a game with under 5 hours logged. |
| `instant_gratification` | Instant Gratification | bronze | false | false | Finish a game within 30 days of adding it. |
| `no_shelf_time` | No Shelf Time | silver | false | false | Finish a game within 7 days of adding it. |
| `long_haul` | Long Haul | silver | false | false | Finish a game that takes 50+ hours to beat. |
| `ultra_marathon` | Ultra Marathon | gold | false | false | Finish a game that takes 100+ hours to beat. |
| `the_commitment` | The Commitment | gold | false | false | Finish 3 games that take 50+ hours to beat. |
| `completionist` | Completionist | silver | false | false | Finish a game having logged its full completion time. |
| `abandonment_issues` | Abandonment Issues | bronze | false | false | Drop a game you've owned for a year or more. Honesty counts. |
| `know_when_to_fold` | Know When to Fold 'Em | silver | true | false | Drop a game after 5+ hours of honest effort. |
| `cut_your_losses` | Cut Your Losses | bronze | true | false | Drop a game with less than 10% of its main story logged. |
| `buyers_remorse` | Buyer's Remorse | bronze | true | false | Drop a game within 7 days of adding it. |
| `wasnt_you_it_was_me` | It Wasn't You, It Was Me | silver | false | false | Drop 5 games. It's not them, it's you. |
| `the_reaper` | The Reaper | gold | false | false | Drop 10 games. |
| `resurrection` | Resurrection | bronze | false | false | Bring a dropped game back into the rotation. |
| `second_chance` | Second Chance | silver | false | false | Resume a game 6+ months after dropping it. |
| `never_give_up` | Never Give Up | gold | false | false | Finish a game you previously dropped. |
| `against_all_odds` | Against All Odds | gold | false | false | Resume a game after a year away and finish it. |
| `phoenix` | Phoenix | legendary | true | false | Return to a game 2+ years after dropping it and finish it. |
| `the_ancient_one` | The Ancient One | silver | false | false | Finish the oldest game you own. |
| `fossil_record` | The Fossil Record | gold | true | false | Finish one of the 3 oldest games you own. |
| `trilogy` | Trilogy | bronze | false | false | Finish 3 games from the same series. |
| `back_to_back` | Back to Back | silver | false | false | Finish two games of the same series in a row, with nothing finished between them. |
| `saga` | Saga | silver | false | false | Finish a game from a series you own 5+ entries of. |
| `franchise_mode` | Franchise Mode | silver | false | false | Finish 5 games from the same series. |
| `marathon_series` | Marathon Series | gold | false | false | Finish 3 games of one series within a calendar year. |
| `closing_the_loop` | Closing the Loop | gold | true | false | Finish the last unplayed game in a series you own — dropped entries don't count against you. |
| `full_set` | The Full Set | legendary | false | false | Finish every game in a series you own (2+ entries, all played). |
| `sampler` | Sampler | silver | false | false | Finish games from 5 different genres in one calendar year. |
| `world_tour` | World Tour | silver | false | false | Finish games on 5 different platforms. |
| `retroactive` | Retroactive | gold | false | false | Finish a game on a platform with no logged session in the last 5 years. |
| `generation_gap` | Generation Gap | gold | false | false | Finish games on 5 different console generations. |
| `nintendo_time_machine` | Nintendo Time Machine | gold | false | false | Finish games on 5 different Nintendo consoles. |
| `the_big_n` | The Big N | legendary | true | false | Finish a game on NES, SNES, N64, GameCube, Wii, Wii U, and Switch. |
| `game_boy_generation` | Game Boy Generation | gold | false | false | Finish a game on Game Boy, Game Boy Color, and Game Boy Advance. |
| `handheld_historian` | Handheld Historian | gold | false | false | Finish games on 4 generations of handhelds. |
| `playstation_pilgrim` | PlayStation Pilgrim | legendary | false | false | Finish a game on PS1, PS2, PS3, PS4, and PS5. |
| `green_across_the_ages` | Green Across the Ages | gold | false | false | Finish games on 4 generations of Xbox. |
| `first_edition` | First Edition | bronze | false | false | Finish your first book. |
| `shelf_improvement` | Shelf Improvement | silver | false | false | Finish 5 books. |
| `well_read` | Well Read | silver | false | false | Finish 10 books. |
| `library_card` | Library Card | gold | false | false | Finish 25 books. |
| `branch_library` | Branch Library | gold | false | false | Finish 50 books. |
| `the_librarian` | The Librarian | legendary | false | false | Finish 100 books. The shelf refills; that's the deal. |
| `late_fine` | Late Fine | silver | false | false | Finish a book you've owned for 5+ years. |
| `third_times_the_charm` | Third Time's the Charm | gold | false | false | Finish a book you started and abandoned twice before. |
| `every_which_way` | Every Which Way | legendary | false | false | Finish one book in all three formats: paper, ebook, and audio. |
| `tbr_trim` | TBR Trim | silver | false | false | Shrink your unread pile by 10 from its peak. |
| `shelf_control` | Shelf Control | gold | false | false | Shrink your unread pile by 25 from its peak. |
| `spark_joy` | Spark Joy | legendary | false | false | Shrink your unread pile by 50 from its peak. |
| `breaking_even` | Breaking Even | gold | true | false | Finish more books in a year than you buy. |
| `doorstop` | Doorstop | silver | false | false | Finish a book over 600 pages long. |
| `honest_dnf` | The Honest DNF | bronze | false | false | Drop a book you've left 'reading' for two years. Honesty counts. |
| `cartographer` | Cartographer | silver | false | false | Map 25 pages of a physical copy by scanning them. |
| `night_owl` | Do You Even Sleep? | bronze | true | true | Log a play session between 3 and 5 in the morning. |
| `hog_watcher` | Hog Watcher | bronze | true | true | Click the Backhog logo 10 times in a row. |
| `konami` | Old Habits | silver | true | true | Enter the Konami code on the achievements page. |
| `queue_shuffler` | Chaos Gremlin | bronze | true | true | Re-queue the same game 5 times in one sitting. |
