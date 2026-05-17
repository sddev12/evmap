# evmap

**A Linux key remapper daemon for games with hard-coded controls**

`evmap` intercepts keyboard input at the OS level and remaps keys according to your configuration—perfect for games that don't allow key rebinding. Remapping is **focus-aware**: it only activates when your target window is in focus, so normal typing is unaffected in all other applications.

---

## 🎯 Motivation

Many games—especially older titles or those from studios like Paradox Interactive (Hearts of Iron IV, Europa Universalis IV, Crusader Kings series) and Total War games—hard-code arrow keys for map scrolling with no in-game rebinding options. If you prefer WASD for map navigation, you're out of luck.

`evmap` solves this by intercepting raw keyboard events, translating them (e.g., `↑ → W`), and emitting the remapped keys through a virtual keyboard. The game sees only the remapped input, and your keybindings work exactly as you want them—no game modification required.

---

## ✨ Features

- **Focus-aware remapping** — Only active when your target window has focus
- **OS-level interception** — Works with any game, no matter how old
- **Multi-compositor support** — X11, Sway, Hyprland, and KWin
- **Hot-reload** — Update your config and reload without restarting (`SIGHUP`)
- **Interactive device selection** — Automatically discovers keyboards or lets you choose
- **No root required** — Just add yourself to the right groups
- **Simple YAML config** — Human-readable key names (`up`, `w`, `space`, etc.)
- **Comprehensive logging** — Configurable log levels for debugging

---

## 🔧 Requirements

- **Linux kernel 2.6+** (evdev and uinput support)
- **curl or wget** (for downloading pre-built binaries)
- **Go 1.26+** (optional; only required if building from source)
- **User permissions**: membership in `input` and `uinput` groups
- **One of the following compositors** (for focus detection):
  - X11 with `xprop` installed
  - Sway with `swaymsg` installed
  - Hyprland with `hyprctl` installed
  - KWin with `qdbus` installed

> **Note:** If you don't specify a `window_title` in your config, evmap will remap keys unconditionally (no focus detection needed), and the compositor requirement doesn't apply.

---

## 📦 Installation

### Automated Installation (Recommended)

The easiest way to install evmap is using the automated installation script, which downloads a pre-built binary for your architecture:

```bash
git clone https://github.com/sddev12/evmap.git
cd evmap
./install.sh
```

The script will:
- ✅ Download the latest pre-built binary (or build from source if unavailable)
- ✅ Install to `/usr/local/bin/`
- ✅ Set up user permissions (input/uinput groups)
- ✅ Configure udev rules
- ✅ Load the uinput kernel module
- ✅ Optionally configure boot loading
- ✅ Verify the installation

**Supported architectures:** linux/amd64, linux/arm64, linux/arm (v7)

**Options:**
```bash
./install.sh -y                  # Non-interactive (accept all defaults)
./install.sh --build-from-source # Build from source instead of downloading
./install.sh --config            # Generate sample config file
./install.sh --no-boot-load      # Skip boot loading setup
./install.sh --help              # Show all options
```

**After installation:** Log out and log back in for group membership to take effect.

---

### Manual Installation

If you prefer manual installation or the script doesn't work for your system:

<details>
<summary>Click to expand manual installation steps</summary>

#### 1. Clone and Build

```bash
git clone https://github.com/sddev12/evmap.git
cd evmap
go build -o evmap .
sudo mv evmap /usr/local/bin/
```

#### 2. Set Up Permissions

To avoid needing root:

```bash
# Add yourself to the input group (for /dev/input access)
sudo usermod -a -G input $USER

# Create uinput group and add yourself (for /dev/uinput access)
sudo groupadd -f uinput
sudo usermod -a -G uinput $USER

# Create udev rule to make /dev/uinput accessible to the uinput group
echo 'KERNEL=="uinput", GROUP="uinput", MODE="0660"' | sudo tee /etc/udev/rules.d/99-uinput.rules

# Reload udev rules
sudo udevadm control --reload-rules
sudo udevadm trigger

# Load uinput module (if not already loaded)
sudo modprobe uinput

# Optional: Make uinput load on boot
echo 'uinput' | sudo tee /etc/modules-load.d/uinput.conf
```

**Log out and log back in** for group membership to take effect.

#### 3. Verify Permissions

```bash
# Check group membership
groups | grep -E 'input|uinput'

# Check /dev/uinput permissions
ls -l /dev/uinput
# Should show: crw-rw---- 1 root uinput ...
```

</details>

---

## 🗑️ Uninstallation

To remove evmap and clean up system configuration:

```bash
./uninstall.sh
```

The script will:
- ✅ Remove the binary from `/usr/local/bin/`
- ✅ Clean up udev rules and boot configuration
- ✅ Optionally remove user from input/uinput groups
- ✅ Optionally remove config file

**Options:**
```bash
./uninstall.sh -y              # Non-interactive (remove everything)
./uninstall.sh --keep-groups   # Keep group memberships
./uninstall.sh --keep-config   # Keep ~/.evmap.yaml
./uninstall.sh --help          # Show all options
```

---

## ⚙️ Configuration

Create a config file at `$HOME/.evmap.yaml` (or use `--config` to specify a different location):

```yaml
# Optional: Set log level (default: INFO)
log_level: DEBUG  # DEBUG | INFO | WARN | ERROR

# Optional: Only remap when this window is in focus
# Omit this section to remap unconditionally
focus:
  window_title: "Hearts of Iron IV"  # Substring match, case-insensitive

# Required: At least one keymap entry
keymaps:
  # Remap arrow keys to WASD for map scrolling
  - from: up
    to: w
  - from: down
    to: s
  - from: left
    to: a
  - from: right
    to: d
```

### Supported Key Names

Key names are case-insensitive. Use human-readable names like `up`, `w`, `space`, etc.

| Category | Keys |
|----------|------|
| **Letters** | `a` `b` `c` `d` `e` `f` `g` `h` `i` `j` `k` `l` `m` `n` `o` `p` `q` `r` `s` `t` `u` `v` `w` `x` `y` `z` |
| **Numbers** | `0` `1` `2` `3` `4` `5` `6` `7` `8` `9` |
| **Arrow Keys** | `up` `down` `left` `right` |
| **Function Keys** | `f1` `f2` `f3` `f4` `f5` `f6` `f7` `f8` `f9` `f10` `f11` `f12` |
| **Navigation** | `home` `end` `pageup` `pagedown` `delete` |
| **Editing** | `enter` `space` `tab` `esc` `backspace` |
| **Modifiers** | `lshift` `rshift` `lctrl` `rctrl` `lalt` `ralt` |
| **Symbols** | `minus` `equal` `backslash` `comma` `dot` `slash` `semicolon` `apostrophe` `leftbrace` `rightbrace` `grave` |

> **Note:** Cannot remap a key to itself (e.g., `from: w, to: w` is invalid).

---

## 🚀 Usage

### Basic Usage

Start evmap (it will auto-discover your keyboard):

```bash
evmap
```

You should see output like:

```
2026/05/17 10:00:00 INFO evmap started
2026/05/17 10:00:00 INFO using input device /dev/input/event3 (AT Translated Set 2 keyboard)
2026/05/17 10:00:00 INFO remapper started keymaps=4
```

### With Custom Config

```bash
evmap --config /path/to/my-config.yaml
```

### With Specific Device

If you have multiple keyboards and want to specify which one to use:

```bash
evmap --device /dev/input/event5
```

### Override Log Level

```bash
evmap --log-level DEBUG
```

### Interactive Device Selection

If multiple keyboards are detected and you're running in a terminal, evmap will prompt you to choose:

```
Multiple input devices found:
1. /dev/input/event2 (AT Translated Set 2 keyboard)
2. /dev/input/event5 (Logitech USB Keyboard)
Select device (1-2): 2
```

---

## 🔄 Hot-Reload Configuration

You can update your config file while evmap is running and reload it without restarting:

```bash
# Edit ~/.evmap.yaml to add/remove/modify keymaps
nano ~/.evmap.yaml

# Send SIGHUP to reload
killall -HUP evmap
# or: kill -HUP $(pidof evmap)
```

You should see:

```
2026/05/17 10:05:00 INFO reloading config
2026/05/17 10:05:00 INFO config reloaded keymaps=6
```

---

## 🛑 Stopping evmap

Press `Ctrl-C` or send `SIGTERM`:

```bash
killall evmap
# or: kill $(pidof evmap)
```

evmap will shut down gracefully:

```
2026/05/17 10:10:00 INFO evmap stopped
```

---

## 🖥️ Compositor Support

evmap automatically detects your desktop environment and uses the appropriate method for focus tracking:

| Compositor | Detection Method | Requirements |
|------------|------------------|--------------|
| **X11** | `DISPLAY` env var | `xprop` command installed |
| **Sway** | `WAYLAND_DISPLAY` + process detection | `swaymsg` command installed |
| **Hyprland** | `HYPRLAND_INSTANCE_SIGNATURE` env var | `hyprctl` command installed |
| **KWin** | D-Bus interface detection | `qdbus` command installed |

If no compositor is detected (or `window_title` is not configured), evmap will remap keys unconditionally—useful for testing or if you want remapping to always be active.

---

## 🐛 Troubleshooting

### "Permission denied" when opening `/dev/input/eventX`

**Solution:** Ensure you're in the `input` group and have logged out/in:

```bash
sudo usermod -a -G input $USER
# Log out and log back in
groups | grep input
```

### "Permission denied" when opening `/dev/uinput`

**Solution:** Ensure you're in the `uinput` group and the udev rule is set up:

```bash
sudo groupadd -f uinput
sudo usermod -a -G uinput $USER
echo 'KERNEL=="uinput", GROUP="uinput", MODE="0660"' | sudo tee /etc/udev/rules.d/99-uinput.rules
sudo udevadm control --reload-rules && sudo udevadm trigger
# Log out and log back in
```

### "No keyboard devices found"

**Solution:** 
1. List all input devices: `ls -l /dev/input/by-path/*kbd*`
2. Check if you have keyboard devices: `evtest` (requires `evtest` package)
3. Use the `--device` flag to specify a device manually

### Keys not remapping in my game

**Possible causes:**

1. **Window title mismatch**: Check that your `window_title` config matches the actual window title.
   ```bash
   # For X11:
   xprop WM_NAME | grep -i "your game"
   
   # For Sway:
   swaymsg -t get_tree | grep -i "your game"
   ```

2. **Compositor not detected**: Run with `--log-level DEBUG` to see focus detection logs:
   ```bash
   evmap --log-level DEBUG
   ```

3. **Permissions issue**: Verify evmap started successfully with no errors.

### Testing without a specific window

Remove or comment out the `focus` section in your config to make remapping active at all times:

```yaml
# focus:
#   window_title: "My Game"

keymaps:
  - from: up
    to: w
```

---

## 🧪 Testing & Development

### Run Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run integration tests (requires /dev/input and /dev/uinput access)
go test -tags=integration ./...
```

### Enable Debug Logging

```bash
evmap --log-level DEBUG
```

This shows detailed information about:
- Device discovery
- Focus state changes
- Every key event processed
- Config reloads

---

## 📁 Project Structure

```
evmap/
├── main.go              # Entry point
├── cmd/
│   ├── root.go          # CLI command implementation
│   └── root_test.go     # CLI tests
├── internal/
│   ├── config/          # Config loading & validation
│   ├── input/           # evdev device handling
│   ├── output/          # uinput virtual keyboard
│   ├── focus/           # Window focus detection
│   └── remapper/        # Core remapping logic
└── specs/               # Functional specifications
    ├── config.md
    ├── input.md
    ├── output.md
    ├── focus.md
    ├── remapper.md
    └── cli.md
```

---

## 🎯 Use Cases

### Paradox Grand Strategy Games

Remap arrow keys to WASD for map scrolling:

```yaml
focus:
  window_title: "Hearts of Iron IV"  # or "Europa Universalis IV", etc.

keymaps:
  - { from: up, to: w }
  - { from: down, to: s }
  - { from: left, to: a }
  - { from: right, to: d }
```

### Total War Games

```yaml
focus:
  window_title: "Total War"  # Matches any Total War title

keymaps:
  - { from: up, to: w }
  - { from: down, to: s }
  - { from: left, to: a }
  - { from: right, to: d }
```

### Custom Gaming Macro

Remap numpad keys to function keys:

```yaml
focus:
  window_title: "My Game"

keymaps:
  - { from: "7", to: f1 }
  - { from: "8", to: f2 }
  - { from: "9", to: f3 }
```

---

## 🤝 Contributing

This project follows **spec-driven development**. Before contributing:

1. Read the relevant spec in `specs/` for the subsystem you're modifying
2. Write tests that codify the spec's behaviour scenarios
3. Implement the feature to satisfy the tests
4. Update the spec if the implementation diverges (with justification)

See the [spec index](specs/README.md) for more details.

---

## 📊 Project Status

**v1.0.0-rc** — Feature complete and ready for testing!

- ✅ All 6 specifications implemented
- ✅ 73 tests passing
- ✅ Multi-compositor support (X11, Sway, Hyprland, KWin)
- ✅ Focus-aware remapping
- ✅ Config hot-reload
- ✅ Interactive device selection
- ✅ Graceful shutdown and signal handling

---

## 📄 License

See [LICENSE](LICENSE) for details.

---

## 🙏 Acknowledgments

Built with:
- [Cobra](https://github.com/spf13/cobra) — CLI framework
- [Viper](https://github.com/spf13/viper) — Configuration management
- [golang.org/x/sys/unix](https://pkg.go.dev/golang.org/x/sys/unix) — Linux evdev & uinput

---

## 📮 Support

- **Issues**: [GitHub Issues](https://github.com/sddev12/evmap/issues)
- **Discussions**: [GitHub Discussions](https://github.com/sddev12/evmap/discussions)

---

**Happy gaming! 🎮**
