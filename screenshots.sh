#!/bin/sh
# Takes the catalog's pictures (listing/cover.png, listing/screenshots/) against
# the demo YouTrack in demo/: a lich backend of its own (LICH_DEV, its own
# config dir, a temporary directory) and lich-native on a virtual X display, so
# no window shows and the lich in use is not touched. Run it after a change to
# the screens, before tagging: the catalog reads listing/ at the release's tag.
#
#   ./screenshots.sh
#
# Needs lich and lich-native on PATH, Xvfb, xdotool, ImageMagick's import and
# sqlite3. The clicks are at fixed points of a 1600x1000 window: a change to
# lich's layout or to the toolbar can move them, so look at every picture.
set -eu

id=youtrack-lich
width=1600
height=1000
demo_addr=127.0.0.1:47920
backend_port=47921
# lich-native draws on the CPU under Xvfb, and the board takes a few seconds.
settle=4

out=$PWD/listing
rig=$(mktemp -d)
cfg=$rig/config/lich
project=$rig/atlas
pids=""

cleanup() {
  for pid in $pids; do kill "$pid" 2>/dev/null || true; done
  # The backend stops its sessions before it exits; wait so the rig can go.
  for pid in $pids; do while kill -0 "$pid" 2>/dev/null; do sleep 0.1; done; done
  rm -rf "$rig"
}
trap cleanup EXIT INT TERM

# Whatever lich session runs this must not leak into the rig.
for v in $(env | sed -n 's/^\(LICH_[A-Z_]*\|CLAUDE[A-Z_]*\)=.*/\1/p'); do unset "$v"; done

mkdir -p "$cfg/extensions-dev/$id" "$cfg/extension-data-dev/$id" "$project" "$out/screenshots"
go build -o "$cfg/extensions-dev/$id/$id" .
cp manifest.json "$cfg/extensions-dev/$id/"
go build -o "$rig/demo" ./demo
printf '{"baseUrl":"http://%s","token":"demo"}\n' "$demo_addr" > "$cfg/extension-data-dev/$id/config.json"
printf '{"cardFields":["Prioridade","Tipo","Estimativa"],"board":"b-1"}\n' > "$cfg/extension-data-dev/$id/settings.json"

"$rig/demo" "$demo_addr" > "$rig/demo.log" 2>&1 &
pids="$pids $!"

backend() {
  rm -f "$cfg/runtime-dev.json"
  # lich opens its home directory as the project, which the pictures show by
  # name; a home of its own also keeps the rig off the agents' files in ours.
  setsid env HOME="$project" XDG_CONFIG_HOME="$rig/config" LICH_DEV=1 LICH_LISTEN_PORT=$backend_port \
    lich --no-window >> "$rig/backend.log" 2>&1 < /dev/null &
  backend_pid=$!
  i=0
  until [ -s "$cfg/runtime-dev.json" ]; do
    i=$((i + 1))
    [ $i -le 100 ] || { echo "backend did not start; see $rig/backend.log" >&2; exit 1; }
    sleep 0.1
  done
}

# A first start creates the database, and the window would then ask to choose
# an agent: the answer is set here before it opens.
backend
kill "$backend_pid"
while kill -0 "$backend_pid" 2>/dev/null; do sleep 0.1; done
sqlite3 "$cfg/lich-dev.db" \
  "INSERT OR REPLACE INTO settings (key, project_id, value) VALUES
     ('provider.default', '', 'claude'), ('tour.basics.seen', '', '1'),
     ('update.patchNotesSeen', '', '$(lich --version | sed -n 's/^lich //p')');"
backend
pids="$pids $backend_pid"

display=90
while [ -e "/tmp/.X$display-lock" ]; do display=$((display + 1)); done
Xvfb ":$display" -screen 0 "${width}x${height}x24" -nolisten tcp > "$rig/xvfb.log" 2>&1 &
pids="$pids $!"
export DISPLAY=":$display"
sleep 1
# A Wayland display that does not exist sends the window to X11, inside Xvfb.
WAYLAND_DISPLAY=none lich-native -runtime "$cfg/runtime-dev.json" -extension "$id/main" \
  -window "${width}x${height}" > "$rig/native.log" 2>&1 &
pids="$pids $!"
sleep $((settle * 3))

# The window takes a press only where the pointer was already seen.
click() { xdotool mousemove "$1" "$2"; sleep 1; xdotool click 1; sleep "$settle"; }
# The pointer rests on an empty corner of the content, where nothing has a tooltip.
shot() { xdotool mousemove 800 930; sleep 1; import -window root "$out/$1"; echo "$out/$1"; }
# A click that missed would leave the screen before it in the picture: what the
# demo was asked for says whether the screen loaded.
asked() {
  [ "$(grep -c "GET /api/$1" "$rig/demo.log")" -ge "${2:-1}" ] || { echo "$3 did not load; see $rig" >&2; exit 1; }
}

asked "agiles/b-1/sprints/s-24?" 1 "the board"
shot screenshots/board.png
cp "$out/screenshots/board.png" "$out/cover.png"
click 800 930 # an empty spot: the first press only focuses the window
click 742 192 # ATL-142, the first card of the second column
asked "issues/ATL-142?" 1 "the card"
shot screenshots/card.png
xdotool key Escape
click 437 70 # Issues
asked "issues?" 1 "the issue list"
shot screenshots/issues.png
click 538 70 # Timesheet
click 282 132 # the week before, whole whatever day this is
asked "workItems?" 2 "the week before"
shot screenshots/timesheet.png
