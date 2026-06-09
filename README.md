# bt-sleepd

Automatically disconnect Bluetooth devices and power off the Bluetooth radio when your Mac sleeps, then re-enable it on wake. This lets your devices (headphones, keyboards, mice, etc.) reconnect instantly to your phone or other machines instead of being held hostage by a sleeping Mac.

## How it works

A small Go binary is triggered by [sleepwatcher](https://www.bernhard-baehr.de/) on macOS sleep/wake events:

- **On sleep** (`~/.sleep`): lists currently connected Bluetooth devices via `blueutil`, disconnects each one, and powers off the Bluetooth radio.
- **On wake** (`~/.wakeup`): powers Bluetooth back on so your Mac's keyboard/mouse can reconnect.

`sleepwatcher` is a tiny daemon that watches for macOS sleep notifications and runs user-defined scripts. It runs as a `launchd` service, so it survives reboots and logins.

## Requirements

- macOS
- [Homebrew](https://brew.sh)
- Go (only if building from source)

## Install

### 1. Install dependencies

```sh
brew install blueutil sleepwatcher
```

### 2. Install `bt-sleepd`

Pick one:

**From source (this repo):**

```sh
git clone https://github.com/yourusername/bt-sleepd.git
cd bt-sleepd
go build -o bt-sleepd .
```

**With `go install`:**

```sh
go install github.com/yourusername/bt-sleepd@latest
```

This puts the binary in `$GOPATH/bin` (default `~/go/bin`).

### 3. Find the binary path

```sh
# If built from source in the repo directory
ls "$(pwd)/bt-sleepd"

# If installed via `go install`
ls "$(go env GOPATH)/bin/bt-sleepd"
```

Remember this path — you'll use it in step 4.

### 4. Create the sleep/wake scripts

Replace `/path/to/bt-sleepd` below with the path from step 3.

`~/.sleep`:

```sh
#!/bin/bash
{
  echo "[$(date)] sleep triggered"
  PATH=/usr/local/bin:/usr/bin:/bin /path/to/bt-sleepd
  echo "[$(date)] exit=$?"
} >> /tmp/bt-sleepd.log 2>&1
```

`~/.wakeup`:

```sh
#!/bin/bash
{
  echo "[$(date)] wakeup triggered"
  /usr/local/bin/blueutil --power 1
  echo "[$(date)] bluetooth re-enabled"
} >> /tmp/bt-sleepd.log 2>&1
```

Make them executable:

```sh
chmod +x ~/.sleep ~/.wakeup
```

The `PATH` prefix on the sleep line is required because `launchd` runs sleepwatcher with an empty `PATH`, so `blueutil` wouldn't be found otherwise. If you installed `blueutil` somewhere other than `/usr/local/bin` (e.g. Apple Silicon Homebrew at `/opt/homebrew/bin`), update the path accordingly:

```sh
which blueutil   # use this output in both scripts
```

### 5. Start sleepwatcher

```sh
brew services start sleepwatcher
```

This installs and loads a `launchd` plist that runs sleepwatcher on every login with:

```
-s ~/.sleep   -w ~/.wakeup
```

### 6. Test

With a Bluetooth device connected:

```sh
pmset sleepnow
```

Your Mac will sleep and wake immediately. Check `/tmp/bt-sleepd.log` — you should see the device disconnected and Bluetooth powered off, then back on after wake.

## Logs

All output is appended to `/tmp/bt-sleepd.log`. Tail it with:

```sh
tail -f /tmp/bt-sleepd.log
```

## Troubleshooting

**"executable file not found in $PATH"** — the `PATH` prefix in `~/.sleep` is missing, or `blueutil` is installed somewhere not in that `PATH`. Run `which blueutil` and update the path in both scripts.

**Sleepwatcher doesn't fire** — verify it's running:

```sh
ps aux | grep sleepwatcher | grep -v grep
```

If not, restart it:

```sh
brew services restart sleepwatcher
```

**Bluetooth doesn't power off** — on recent macOS, `blueutil` (and any binary that controls Bluetooth) may need to be granted Accessibility / Automation permissions in System Settings → Privacy & Security. Since sleepwatcher runs under `launchd`, you may also need to grant the same to `/usr/local/opt/sleepwatcher/sbin/sleepwatcher` or `Terminal`.

**Want to uninstall** — stop the service and remove the scripts:

```sh
brew services stop sleepwatcher
rm ~/.sleep ~/.wakeup
```

## License

See [LICENSE](LICENSE).
