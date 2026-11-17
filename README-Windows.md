Trayscale for Windows
=====================

A native **Windows** port of [Trayscale](https://github.com/DeedleFake/trayscale).

The upstream project is a GTK4/libadwaita GUI built for Linux and does not run on
Windows. This port keeps Trayscale's cross-platform core (`internal/tsutil`,
which talks to the Tailscale LocalAPI) and re-implements the UI with **pure-Go**
toolkits — no GTK, no CGO, no MSYS2. There are two editions:

| Binary | UI | Toolkit |
|--------|----|---------|
| `dist\Trayscale-GUI.exe` (~28 MB) | Full **window**: machine sidebar + detail pane with IPs and option toggles, reproducing the upstream libadwaita layout | [Gio](https://gioui.org) |
| `dist\Trayscale.exe` (~22 MB) | System-**tray** icon + menu | [`fyne.io/systray`](https://github.com/fyne-io/systray) |
| `dist\Trayscale-Doctor.exe` | Console diagnostic for LocalAPI problems (no UI) | — |

Pick whichever you prefer — they are independent and use the same core.

GUI edition (window)
--------------------

`dist\Trayscale-GUI.exe` opens a window that mirrors the original Trayscale and
adds the owner-based grouping of the official Windows client:

* A left **navigation rail** with **Devices**, **Exit Nodes** and **Settings**
  views (like the macOS client).
* A **connect toggle**, your tailnet login, and the current **exit node** in the
  header.
* A **searchable sidebar** (type to filter) with machines **grouped by owner** —
  *This machine*, a *My devices* group, then one group per other user
  (`banganh`, `dangnguyen`, …), just like the official Windows menu. Each row
  shows the device's Tailscale IP and a small green/grey presence dot.
* A scrollable **detail pane** for the selected machine, combining the Linux and
  macOS layouts:
  * A **status dot** (Connected / Not Connected) under the name.
  * **Tailscale addresses** — MagicDNS name, IPv4 and IPv6, each labelled with a
    **Copy** button (macOS style).
  * **Options** (this machine) — *Advertise exit node*, *Allow LAN access*,
    *Accept routes*, *Accept DNS*, each with its description. For a peer that can
    be an exit node, a *Use as exit node* switch instead.
  * **Taildrop** (peers) — **Choose a File…** opens the native picker, or drag
    files straight from Explorer onto the window. Drop targets the device
    currently selected in the sidebar. Sending shows a **full-width blue
    progress line** with a live percentage and Cancel; multiple files are queued
    and reported in a toast. Taildrop and Ping are hidden when Tailscale is
    disconnected or the selected device is offline — an explanatory card takes
    their place.
  * **Ping** (peers) — **Ping device** / **Stop** runs a continuous ping,
    showing the connection type (**Direct** green / **DERP-relayed** orange), the
    live latency, and a smoothed, auto-scaled latency graph (macOS-style).
    Only offered for online devices.
  * **Files** (this machine) — pending Taildrop files (Delete), or "No incoming
    files."
  * **Advertised Routes** (this machine) — list with **+** to add and **Remove**.
  * **Network Check** (this machine) — a **↻** button runs a netcheck (UDP,
    IPv4/IPv6, preferred DERP, per-region latency).
  * **Details** — OS, key expiry, created, last seen.
* The **Exit Nodes** view lists exit-capable peers with *None*, a checkmark on the
  active one, and an *Allow local network access* toggle.
* The **Settings** view has real, persisted preferences: *Start with Windows*
  (current-user Run key), *Show offline devices* in the sidebar, and *Compact
  device list*. It also shows the LocalAPI socket, connection state and drag &
  drop mode, with shortcuts to **Open log file** and **Run diagnostics** (when
  `Trayscale-Doctor.exe` sits beside the app).

Tray edition
------------

From the tray icon you can:

* See connection state at a glance — the icon changes for **disconnected**,
  **connected**, and **connected via exit node**.
* **Connect / Disconnect** (`tailscale up` / `down`), or **Log in** when needed.
* Pick an **exit node** from the list of exit-capable peers, or set it to *None*.
* Toggle **Allow local network access**, **Run as exit node**, **Accept subnet
  routes**, and **Accept DNS**.
* Browse **Peers** and click one to copy its Tailscale IP to the clipboard.
* Copy **this machine's** Tailscale IP (click the top menu entry or left-click
  the tray icon for a status summary).
* Open the **Admin console** in your browser.

Requirements
------------

* Windows 10 / 11 (x64).
* The official **Tailscale for Windows** installed and signed in
  (`tailscaled` runs as a service). Download: <https://tailscale.com/download/windows>.

The app talks to the running Tailscale service over its LocalAPI as the current
user — no elevation is needed for reading status or for the usual
connect/disconnect/exit-node actions.

### If the app says "Waiting for Tailscale…" or "Can't reach the Tailscale service"

The window and the tray now show the actual LocalAPI error instead of waiting
forever. Run **`Trayscale-Doctor.exe`** from a console (or double-click it) for a
full diagnosis. It prints:

1. the LocalAPI socket/pipe in use,
2. whether `GET /localapi/v0/status` succeeds,
3. whether the IPN-bus watch (`watch-ipn-bus`) succeeds with the modern
   `NotifyInitialStatus`/`NotifyPeerChanges` mask,
4. the output of `tailscale status`.

Common causes:

* **Tailscale is not running** — start the Tailscale service or the app.
* **The app was built against an old Tailscale API** — versions before this fix
  subscribed to `NotifyInitialNetMap`, which Tailscale 1.104+ rejects with HTTP
  400. Rebuild with the current tree (see below).
* **Unusual socket** — a `tailscaled` started by hand (for example with
  `--windows-mode=dev`) uses a per-user pipe instead of the service pipe. Set
  `TRAYSCALE_LOCALAPI` to that pipe path before starting either edition:

  ```powershell
  $env:TRAYSCALE_LOCALAPI = '\\.\pipe\tailscale-<your-SID>'
  .\Trayscale-GUI.exe
  ```

  The doctor prints the path it uses and honors the same variable.

Install / Run
-------------

Prebuilt binaries are in `dist\`. Double-click **`Trayscale-GUI.exe`** for the
window, or **`Trayscale.exe`** for the tray icon (you may need to expand the
hidden-icons `^` area and drag it onto the taskbar). Quit the tray edition from
its menu → **Quit**; close the window edition with its title-bar ✕.

### Start automatically with Windows

The GUI's **Settings → Start with Windows** toggle registers (or removes) the
current-user Run key automatically. Alternatively, put a shortcut in your
Startup folder (swap the exe name for whichever edition you want to autostart):

```powershell
$exe = "H:\Claude\trayscale\dist\Trayscale-GUI.exe"   # or dist\Trayscale.exe
$startup = [Environment]::GetFolderPath('Startup')
$s = (New-Object -ComObject WScript.Shell).CreateShortcut("$startup\Trayscale.lnk")
$s.TargetPath = $exe
$s.WorkingDirectory = Split-Path $exe
$s.Save()
```

Remove it later by deleting `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\Trayscale.lnk`.

Build from source
-----------------

Requires Go >= 1.27.3 (set by the `tailscale.com` dependency). Check `go.mod`
for the exact toolchain.

```powershell
powershell -ExecutionPolicy Bypass -File .\build-windows.ps1
```

This regenerates each edition's embedded Windows manifest (`rsrc_windows_amd64.syso`,
via `github.com/akavel/rsrc`) if needed and produces `dist\Trayscale-GUI.exe`,
`dist\Trayscale.exe`, and `dist\Trayscale-Doctor.exe`.

To cross-compile the same binaries from Linux or macOS, use the shell
script instead (the embedded `.syso` resources are checked in):

```sh
./build-windows.sh
```

Both scripts set `CGO_ENABLED=0`, `GOOS=windows`, and `GOARCH=amd64`; nothing
outside the Go toolchain is required.

> **The manifest is required.** Tailscale's Windows version detection
> (`github.com/dblohm7/wingoes`) panics with *"incoherent Windows version --
> missing/outdated manifest"* unless the executable embeds an application
> manifest declaring `supportedOS` for Windows 10/11. See
> `cmd\trayscale-win\app.manifest`.

### Building the GUI on Linux

The window edition is [Gio](https://gioui.org), which is cross-platform, so it
builds and runs natively on **Linux** too — the same window, grouping, Taildrop,
Ping and Exit-Nodes UI, talking to the local `tailscaled`. Install Go >= 1.27.3
and Gio's Linux dev libraries, then build just the GUI:

```sh
sudo apt install gcc pkg-config libwayland-dev libx11-dev libx11-xcb-dev   libxkbcommon-x11-dev libgles2-mesa-dev libegl1-mesa-dev libffi-dev   libxcursor-dev libvulkan-dev wayland-protocols
go build -o dist/trayscale-gui ./cmd/trayscale-gui
```

The Taildrop file picker uses the desktop's own chooser (`zenity` on GNOME,
`kdialog` on KDE) through `internal/winui/dialog_other.go`. The tray edition
(`cmd/trayscale-win`) stays Windows-only; on Linux use upstream Trayscale's tray.

Logs
----

Runtime logs (state changes, action errors) are written to:

```
%APPDATA%\Trayscale\trayscale-gui.log   (window edition)
%APPDATA%\Trayscale\trayscale.log       (tray edition)
```

Notes & limitations
-------------------

* Mullvad exit nodes are omitted from the lists to keep them short. The tray's
  exit-node and peer lists are capped (50 / 100 entries).
* Multi-profile switching from the original UI is not exposed.
* Taildrop sends files (one at a time, queued when several are dropped); incoming
  files can be listed and deleted, but auto-save is not exposed. The drop target
  is installed via native Win32 on Gio's window. The default mode is the
  shell `WM_DROPFILES` path (no COM). Set `TRAYSCALE_OLE_DND=1` to enable an
  OLE `IDropTarget` as well, which adds a drag-over highlight but requires COM
  on the window thread. The chosen mode and startup progress are logged in
  `%APPDATA%\Trayscale\trayscale-gui.log`; if the window ever hangs, that log
  shows the last stage reached.
* NetCheck is available in the window edition only.
* UI strings are English-only; the upstream gettext catalogs are not wired into
  the Windows front-ends.
* The window edition's title bar uses standard Windows decorations rather than
  the upstream client-side (libadwaita) header bar.

Layout of the Windows-specific code
-----------------------------------

| Path | Purpose |
|------|---------|
| `cmd/trayscale-gui/` | Window entry point + `app.manifest` + generated `.syso` |
| `internal/winui/`    | Gio window — sidebar + detail pane, wired to `tsutil` |
| `cmd/trayscale-win/` | Tray entry point + `app.manifest` + generated `.syso` |
| `internal/wintray/`  | Tray icon + menu, wiring the `tsutil` core to systray |
| `cmd/trayscale-doctor/` | Console diagnostic for LocalAPI/socket problems |
| `internal/tsutil/`   | Tailscale LocalAPI client + IPN poller (upgraded to the post-1.104 status/peer-delta bus API) |
| `build-windows.ps1`  | One-shot build script (both editions + doctor) |
