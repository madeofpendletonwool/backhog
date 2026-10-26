# E-readers: KOReader and CrossPoint

Backhog speaks two standard protocols, so an e-reader can use your library
directly:

- **OPDS** (`/api/opds`): browse and search the library, and download books.
  The device gets the original EPUB, byte for byte.
- **KOReader sync** (`/api/kosync`): the device pushes where you are and pulls
  where Backhog is. Pushes update the same position the web reader and the
  Android app use, so you can switch between them freely.

This works with **KOReader** on Kindle, Kobo, PocketBook, Android and desktop,
and with **CrossPoint** firmware on Xteink readers. Audiobooks are not part of
it.

## 1. Make a device token

In Backhog, open **Settings → API tokens**:

1. Name the token after the device (for example, `kindle`).
2. Tick **This is an e-reader**.
3. Click **Create**.

The card that appears shows the catalog and sync addresses and your username.
The key is the password for both. It is shown only once.

**Custom key:** the generated key is 67 characters, which is miserable on an
e-reader's on-screen keyboard. Fill in **Custom key** before creating to pick
your own — `bh_x4-pro`, say. It must start with `bh_` and use 4–120 letters,
numbers, `-`, `_` or `.` after it, and no other token may already hold it. A
short key is easier to guess than the generated one; on your own network, over
TLS, that is usually the right trade. Type it once on the device instead of
seventy tiny taps.

An e-reader token can read your library and move your reading position, and
nothing else. Revoke it from the same page if the device is lost.

## 2. KOReader

### Catalog

1. Open the **Search** menu (magnifying glass) and choose **OPDS catalog**.
2. Tap **+** and fill in:
   - **URL:** `https://your-backhog/api/opds`
   - **Username:** your username
   - **Password:** the key

Open **Backhog** to see these shelves:

- Reading now
- Queue
- Recently added
- All books
- Lists
- Series

Search covers title, author, series name and ISBN. Tap a book and choose the
EPUB to download it.

### Progress sync

1. Open a book.
2. Go to **Tools (wrench) → Progress sync → Custom sync server** and enter
   `https://your-backhog/api/kosync`.
3. Choose **Login**, with your username and the key as the password.
   **Register** always fails on purpose: accounts are made in Backhog.
4. Set **Document matching method** to **Binary**. This is the default, and it
   is reliable for books downloaded from the catalog.

These settings are recommended:

- **Auto sync:** on. KOReader then pushes every N pages and when the device
  sleeps, and pulls when you open a book.
  - On a Kindle, auto sync also needs `wifi_enable_action = "turn_on"`.
    Without it, KOReader turns auto sync off.
  - If the Wi-Fi wake-ups feel slow, raise **Sync every # pages**.
- **Sync to a newer state:** silently. Opening a book you read further in the
  web reader then jumps forward.
- **Send metadata:** on. Backhog can then recognise books you copied onto the
  device by hand (see Troubleshooting).

**One-gesture manual sync:** go to **Settings → Taps and gestures → Gesture
manager**, pick a gesture, and assign **Reader → Push progress from this
device**. Use it before you put the device down.

### Setting up a Kindle over USB

With the Kindle mounted and KOReader closed, you can write these settings
instead of typing them. The files are under `koreader/` on the device.

`settings/opds.lua`:

```lua
return {
    ["servers"] = {
        [1] = {
            ["title"] = "Backhog",
            ["url"] = "https://your-backhog/api/opds",
            ["username"] = "you",
            ["password"] = "bh_…",
        },
    },
}
```

`settings/kosync.lua`. In `userkey`, put the MD5 hex of the key: run
`printf %s bh_… | md5`, or `md5sum` on Linux.

```lua
return {
    ["settings"] = {
        ["custom_server"] = "https://your-backhog/api/kosync",
        ["username"] = "you",
        ["userkey"] = "<md5 of the key>",
        ["auto_sync"] = true,
        ["pages_before_update"] = 5,
        ["checksum_method"] = 0,
        ["sync_forward"] = 2,
        ["sync_backward"] = 3,
        ["send_metadata"] = true,
    },
}
```

Then add `["wifi_enable_action"] = "turn_on",` near the top of
`settings.reader.lua`. Back the file up first.

## 3. CrossPoint (Xteink)

### Catalog

Go to **Settings → System → OPDS Servers → Add Server** and enter:

- **URL:** `https://your-backhog/api/opds`
- **Username:** your username
- **Password:** the key

CrossPoint's browser is a list of titles; it does not show covers.

### KOReader Sync

Enter the server URL `https://your-backhog/api/kosync`, then your username and
the key. These settings are recommended:

- **Server type: Other.** This sends richer positions and the ISBN.
- **Match method: Binary.**
- **Sync behaviour: Smart.**

**Sync is manual on CrossPoint.** Use **Sync Progress** in the reader menu, or
set the long-press shortcut to it. Get in the habit of syncing before you put
the device down.

## How it works

- **The catalog** lists only books whose primary text is an EPUB you can read:
  books you own, or books someone has shared with you. Pages hold 25 entries.
  Links are absolute; they come from `PUBLIC_URL` when it is set, otherwise
  from the proxy headers.
- **The download** is the original file. A sync client identifies a book by a
  hash of its bytes (or of the filename CrossPoint saved it as), so Backhog
  records those hashes each time a book is downloaded. On an unknown hash it
  hashes the rest of your library on demand.
- **Positions** convert between KOReader's XPointers and Backhog's character
  offsets at paragraph precision. Backhog walks each chapter with the same
  rules that built its text (`books.Locator`). When Backhog has not moved since
  a device's push, a pull hands the device back its own exact position.
- **Reading sessions** are estimated: two pushes from the same device between
  a minute and four hours apart, with the position moving forward.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `401` / "Unauthorized" | The key is wrong or revoked, or the username isn't the token owner's. Check the token in Settings. |
| `403` on push | The token isn't an e-reader token (it lacks `progress:write`). Make a new one with the box ticked. |
| Login works on CrossPoint but not KOReader | The token wasn't created as an e-reader token. Only e-reader tokens accept KOReader's MD5 login. |
| A book syncs but Backhog never moves | Its hash matches nothing, for example a book copied by hand. It appears under **Settings → Unmatched e-reader syncs**; link it to the right book. |
| Catalog "next page" fails behind a proxy | Links must be `https`. Make sure your TLS proxy sends `X-Forwarded-Proto` (the bundled nginx passes it through), or set `PUBLIC_URL`. |

## Testing without a device

`devices/crosspoint-harness` builds CrossPoint's own OPDS parser and
document-id code for your computer. The `scripts/ereader` scripts drive it:

```sh
make -C devices/crosspoint-harness
scripts/ereader/check-catalog.sh https://your-backhog/api/opds you bh_…
```

`check-catalog.sh` walks every shelf and page through CrossPoint's parser and
fails on a parse error, truncation, a missing search link, or an `http://`
link.

To compare the sync server against KOReader's reference server:

```sh
docker run -d -p 17200:7200 koreader/kosync:latest
scripts/ereader/kosync-diff.py --ref https://localhost:17200 \
  --backhog https://your-backhog/api/kosync --user you --token bh_… --document <hash>
```

`scripts/ereader/xpresolve.py book.epub '<xpointer>'` prints the text at an
XPointer the way an XML-parsing engine (KOReader's crengine) sees it. To check
pointers a real device produced, collect `last_xpointer` from
`<book>.sdr/metadata.epub.lua` on the device. Then run
`BACKHOG_DEVICE_GOLDENS=manifest.json go test ./internal/books -run DeviceGoldens`
(see that test for the manifest format).
