# Release QA checklist

The release gate for Backhog for Android. Walk this on a real device against
your production server before publishing a tag; every box should end up ticked
or struck through with a reason. The deferred list at the bottom is scope that
is *deliberately* absent — anything missing that is neither ticked nor on that
list is a bug.

Setup: a signed release APK from a tag build (or the run's artifact), a device
running a recent Android, and your production server. The reader-role pass
needs a second account with the `reader` role.

## Install

- [ ] Install via `adb install -r backhog-<version>.apk`
- [ ] Install via file manager (allow "unknown sources" for that app when prompted)
- [ ] Install-over-update from the previous version: session survives, no login
      prompt; theme choices, remembered filters, and playback speed survive
- [ ] Cold start after force-stop resumes the session (no login prompt)
- [ ] Logout → login works; switch server works

## Games arena

- [ ] Library grid ↔ table, facet filters, status filter, sort — and they
      survive app restart
- [ ] Covers load with the accent tint
- [ ] Search → add to library (backlog) and to wishlist
- [ ] Game detail dossier renders; status changes, rating, notes, playing-on,
      sessions add
- [ ] Queue: drag reorder, jump top/bottom, nudge, mark finished (→
      `next_up` toast on a fresh account)
- [ ] Lists: create/rename/delete, drag-sort manual lists, add/remove entries
      from list detail and from game detail
- [ ] Smart list builder: same membership as the web for the same rules
- [ ] Series: play-order switch (release / chronological / recommended /
      custom / good-ones), backfill trigger
- [ ] Projects: CRUD, item toggle/reorder
- [ ] Dashboard + debt report match the web numbers
- [ ] Tonight picks + random roll show reasons
- [ ] Steam import: preview → select rows → bulk add
- [ ] Achievements: gallery, hidden entries masked, season card, unlock toasts

## Books arena

- [ ] Library, stats, filters with remembered state
- [ ] Add via Open Library search, typed ISBN
- [ ] Add via camera barcode scan (real EAN on a real paperback)
- [ ] Book detail: dossier, status/rating/notes, sessions, files summary,
      share panel, delete
- [ ] Position view: percentage, audio timecode, page with the right error
      form (`page 214`, `page 214 ± 3`, `page ~214`)
- [ ] Copies: register printing (owned/borrowed), return / re-open / mark-owned
      keep page anchors, delete drops them
- [ ] Page scanning: photograph a real page → passage match → anchor pinned →
      position reports the page as exact
- [ ] Search in book: three-coordinate results; jump lands in the reader /
      player at the right spot
- [ ] Reading dashboard + Reading Season
- [ ] Lending out: share a book; the recipient sees it in Settings → shared
      books, adds it, keeps independent progress; unshare keeps their progress

## Reader

- [ ] Open an EPUB: chapters drawer (incl. a no-TOC book's explanation),
      illustrations render
- [ ] Page turns write position; force-stop mid-chapter → reopen lands on the
      same paragraph the web reader would
- [ ] Resume from server position; progress by chapter + percent
- [ ] Reading theming follows the theme family (Paper light, dark in Midnight)

## Player and the car

- [ ] Play an m4b: background/screen-off playback, notification controls,
      audio focus ducks for navigation and calls
- [ ] Deep seek into a 400MB m4b is quick (byte-range honored)
- [ ] Speed persists across books and restarts; mid-chapter speed change keeps
      position exact
- [ ] Force-stop mid-playback → reopen → server position reflects the last write
- [ ] Listen → read handoff: stop mid-sentence, open the reader, lands at the
      matching paragraph
- [ ] Android Auto Desktop Head Unit: Backhog appears as a media source;
      browse tree loads; play/pause/seek/next-prev/speed/metadata correct

## File layer (member account)

- [ ] Scan NAS → review candidates → attach EPUB + audiobook → promote
      primaries (differing-text warning shown) → queue alignment, watch it
      complete → detach
- [ ] Missing-NAS state explains the drive-offline case, destroys nothing

## Reader-role account

- [ ] Everything above works except the file layer: no scan, no attach/detach,
      no primary promotion, no alignment anywhere in the UI
- [ ] Audio playback is allowed (reader can listen — same as web)

## Eggs

- [ ] Night owl: log a session between 3:00 and 4:59 AM local
- [ ] Hog watcher: ten taps on the Backhog logo
- [ ] Queue shuffler: same game moved to top of queue five times in one sitting
- [ ] No Konami code egg (deliberate — see deferred list)

## Themes

- [ ] Midnight and Library (Paper / Hearth) across both arenas
- [ ] Per-arena theme choice persists across restart

## Degraded modes

Check against a server (or server config) with each capability absent — the
rest of the app must keep working:

- [ ] No IGDB creds: add-game search disabled with the clear message
- [ ] No Steam key: import surfaces the server's no-key message
- [ ] No align worker: alignment job sits queued, honest state, no hangs
- [ ] NAS unmounted: missing-files handling as above

## Deliberately deferred (missing is not a bug)

- **Arcade theme family** — pixel-art web theme, later pass
- **Admin panel** — users, invites, server settings stay on the web
- **Konami code egg** — not ported
- **CarPlay** — not possible from an Android APK; Android Auto is the car
  story (decision record in the README)
