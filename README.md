# youtrack-lich

YouTrack inside [lich](https://github.com/lichdotdev/lich): the agile board, the issue list, a card's own screen
and the weekly timesheet, as a lich extension.

An unofficial client: not affiliated with or endorsed by JetBrains. YouTrack is a trademark of JetBrains s.r.o.

## What it does

- **Board**: pick a board and a sprint, drag a card to another column to change its state, choose which custom
  fields a card shows. The board on screen is fetched again every minute.
- **Issues**: a YouTrack search, with your saved searches one click away.
- **Card**: title and description (Markdown, with a preview), tags, links and attachments, and the project's
  fields down the right: single and multiple choice, users and periods are editable in place. Comments, logged
  time and history each have a tab.
- **New card**: the same screen, empty, with the fields of the project you pick.
- **Timesheet**: what you logged in a week, by card and by day, and a row to log more.
- **Open session**: starts a lich session in one of your open projects with the card as its first prompt.

| Key | Does |
| --- | --- |
| `1` `2` `3` | Board, Issues, Timesheet |
| `/` | Puts the keyboard in the search field |
| `c` | New card |
| `r` | Fetch again |
| `Esc` | Back from a card |
| `Ctrl+S` | Save the title and description being edited |
| `Ctrl+Enter` | Send the comment, or create the card |

## Install

In lich's Settings, under Extensions, install from GitHub and paste `https://github.com/omartelo/youtrack-lich`.
It needs lich 0.90.1 or newer.

On first open, give it the URL of your YouTrack and a
[permanent token](https://www.jetbrains.com/help/youtrack/server/manage-permanent-token.html). Both are kept
on your machine only, in the extension's data directory, in a file only you can read.

**Insecure connection** skips the verification of the server's certificate, for an instance behind a CA your
machine does not trust. Whoever sits between you and the server can then read the token: prefer adding the CA
to your system.

## Limits

- A period (an estimate) is typed in hours and minutes, such as `4h 30m`: days and weeks depend on the
  instance's working hours.
- Comments and logged time can be added, not yet edited or removed.
- Time logged from the timesheet carries no activity type; the card's own tab does.
- Dates and text fields of a card are shown, not edited.
- The interface is in Portuguese.

## Build

```sh
go build -o youtrack-lich .
ln -s "$PWD" ~/.config/lich/extensions/youtrack-lich   # then restart lich
go test ./...
```

`./release.sh vX.Y.Z` builds the archives and `checksums.txt` a release carries.

`go run ./demo` serves the reads of a YouTrack with an invented team on `127.0.0.1:47920`, for trying a screen
without a real instance. `./screenshots.sh` runs the extension against it in a lich of its own, on a virtual display,
and writes the catalog's cover and screenshots to `listing/`: run it after changing a screen, before tagging, since the
catalog reads `listing/` at the release's tag. It needs Xvfb, xdotool, ImageMagick and sqlite3.

## License

MIT
