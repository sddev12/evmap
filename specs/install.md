# Spec: Installation and Uninstallation Scripts

## Overview

This specification defines two shell scripts that automate the installation and removal of `evmap` on Linux systems. The scripts handle building, permissions setup, system configuration, and verification—eliminating manual steps from the README.

---

## Goals

- Provide a single-command installation experience
- Automate all permission and group setup
- Configure system resources (udev, kernel modules) correctly
- Verify installation success before completing
- Allow clean uninstallation with no leftover artifacts
- Be idempotent (safe to run multiple times)
- Provide clear, colored output for user feedback
- Handle errors gracefully with helpful messages

---

## Scripts

### 1. Install Script (`install.sh`)

**Purpose:** Build evmap, install the binary, configure permissions, and verify the installation.

**Requirements:**
- Must run on Linux (kernel 2.6+)
- Requires Go 1.26+ to be installed
- Requires sudo for system modifications
- Safe to run multiple times (idempotent)

**User Interaction:**
- Non-interactive by default
- Optional `--yes` or `-y` flag to skip all prompts
- Optional `--no-boot-load` flag to skip uinput boot loading
- Optional `--config` flag to generate sample config file

---

### 2. Uninstall Script (`uninstall.sh`)

**Purpose:** Remove evmap binary, udev rules, boot configuration, and optionally remove user from groups.

**Requirements:**
- Must run on Linux
- Requires sudo for system modifications
- Safe to run even if evmap is not fully installed

**User Interaction:**
- Prompts for confirmation before removal
- Optional `--yes` or `-y` flag to skip all prompts
- Optional `--keep-groups` flag to keep user in input/uinput groups

---

## Install Script Workflow

### Phase 1: Pre-flight Checks

**Steps:**

1. **Verify Linux OS**
   - Check `uname -s` returns `Linux`
   - Exit with error if not Linux

2. **Check Go installation**
   - Check `go version` succeeds
   - Verify version >= 1.26
   - Exit with error if Go not found or version too old

3. **Check for evmap binary in current directory**
   - Look for `evmap` binary in current directory
   - If exists, offer to skip build step
   - If not exists or user declines, proceed to build

4. **Check if already installed**
   - Check if `/usr/local/bin/evmap` exists
   - If exists, show version and offer to reinstall or upgrade

### Phase 2: Build

**Steps:**

1. **Clean previous builds** (optional)
   - Remove existing `evmap` binary from current directory if present

2. **Build binary**
   - Run `go build -o evmap .`
   - Check exit code
   - Exit with error if build fails

3. **Verify binary**
   - Check `evmap` file exists and is executable
   - Run `./evmap --version` to verify it works (if --version implemented)

### Phase 3: Installation

**Steps:**

1. **Install binary**
   - Copy `evmap` to `/usr/local/bin/evmap` (requires sudo)
   - Set ownership to `root:root`
   - Set permissions to `755` (executable by all, writable by root)
   - Verify `/usr/local/bin/evmap` exists after copy

2. **Verify PATH**
   - Check if `/usr/local/bin` is in `$PATH`
   - Warn user if not in PATH

### Phase 4: Permissions Setup

**Steps:**

1. **Add user to input group**
   - Check if user already in `input` group (`groups` command)
   - If not, run `sudo usermod -a -G input $USER`
   - Verify user added to group

2. **Create uinput group**
   - Check if `uinput` group exists (`getent group uinput`)
   - If not, run `sudo groupadd uinput`
   - Verify group created

3. **Add user to uinput group**
   - Check if user already in `uinput` group
   - If not, run `sudo usermod -a -G uinput $USER`
   - Verify user added to group

### Phase 5: System Configuration

**Steps:**

1. **Create udev rule**
   - Check if `/etc/udev/rules.d/99-uinput.rules` exists
   - If exists, compare content with expected rule
   - If missing or different, create/update rule:
     ```
     KERNEL=="uinput", GROUP="uinput", MODE="0660"
     ```
   - Reload udev rules: `sudo udevadm control --reload-rules`
   - Trigger udev: `sudo udevadm trigger`

2. **Load uinput module**
   - Check if uinput module loaded: `lsmod | grep uinput`
   - If not loaded, run `sudo modprobe uinput`
   - Verify module loaded

3. **Configure boot loading** (optional, prompted)
   - Ask user if they want uinput to load on boot
   - If yes, check if `/etc/modules-load.d/uinput.conf` exists
   - If not, create it with content: `uinput`
   - Skip if `--no-boot-load` flag provided

### Phase 6: Verification

**Steps:**

1. **Verify binary installation**
   - Check `which evmap` returns `/usr/local/bin/evmap`
   - Run `evmap --version` if available

2. **Verify group membership**
   - Run `groups` and check for `input` and `uinput`
   - Note: New group membership requires logout/login

3. **Verify /dev/uinput permissions**
   - Check `ls -l /dev/uinput` shows `crw-rw---- ... root uinput`
   - Verify group is `uinput` and permissions are `0660`

4. **Verify uinput module**
   - Check `lsmod | grep uinput` shows module loaded

### Phase 7: Completion

**Steps:**

1. **Display summary**
   - List all completed steps
   - Show any warnings or skipped steps

2. **Show next steps**
   - Remind user to **log out and log back in** for group membership
   - Show how to create config file (`~/.evmap.yaml`)
   - Show example config snippet
   - Show how to run evmap

3. **Optional: Generate sample config**
   - If `--config` flag provided, create `~/.evmap.yaml` with example content
   - Do not overwrite if already exists (prompt first)

---

## Uninstall Script Workflow

### Phase 1: Pre-flight Checks

**Steps:**

1. **Verify Linux OS**
   - Check `uname -s` returns `Linux`
   - Exit with error if not Linux

2. **Confirm uninstallation**
   - Prompt user: "This will remove evmap and its configuration. Continue? [y/N]"
   - Skip prompt if `--yes` or `-y` flag provided
   - Exit if user declines

### Phase 2: Remove Binary

**Steps:**

1. **Check if binary exists**
   - Check if `/usr/local/bin/evmap` exists
   - If not, skip this phase and warn user

2. **Remove binary**
   - Run `sudo rm -f /usr/local/bin/evmap`
   - Verify binary removed

### Phase 3: Remove System Configuration

**Steps:**

1. **Remove udev rule**
   - Check if `/etc/udev/rules.d/99-uinput.rules` exists
   - If exists, run `sudo rm -f /etc/udev/rules.d/99-uinput.rules`
   - Reload udev: `sudo udevadm control --reload-rules`
   - Trigger udev: `sudo udevadm trigger`

2. **Remove boot loading config**
   - Check if `/etc/modules-load.d/uinput.conf` exists
   - If exists, run `sudo rm -f /etc/modules-load.d/uinput.conf`

3. **Unload uinput module** (optional)
   - Check if uinput module is loaded
   - Ask user if they want to unload it now (may be used by other tools)
   - If yes, run `sudo modprobe -r uinput`
   - Ignore errors (module may be in use)

### Phase 4: Group Cleanup

**Steps:**

1. **Prompt for group removal**
   - Ask: "Remove user from input and uinput groups? [y/N]"
   - Skip if `--keep-groups` flag provided
   - Default: No (groups may be used by other applications)

2. **Remove from input group** (if user confirms)
   - Run `sudo gpasswd -d $USER input`
   - Note: Requires logout/login to take effect

3. **Remove from uinput group** (if user confirms)
   - Run `sudo gpasswd -d $USER uinput`
   - Note: Requires logout/login to take effect

4. **Remove uinput group** (if user confirms and no other members)
   - Check if uinput group has other members
   - If no other members, ask to delete group
   - Run `sudo groupdel uinput`

### Phase 5: Config File Cleanup

**Steps:**

1. **Prompt for config removal**
   - Ask: "Remove ~/.evmap.yaml config file? [y/N]"
   - Default: No (user may want to keep their configuration)

2. **Remove config** (if user confirms)
   - Run `rm -f ~/.evmap.yaml`
   - Also check for `.evmap.yaml` in current directory

### Phase 6: Completion

**Steps:**

1. **Display summary**
   - List all removed items
   - List any items skipped or kept

2. **Show next steps** (if groups removed)
   - Remind user to log out and log back in

---

## Output Format

### Color Coding

- **Green**: Success messages, checkmarks
- **Yellow**: Warnings, prompts, informational messages
- **Red**: Errors, failures
- **Blue**: Section headers
- **Cyan**: Commands being executed (when verbose)

### Message Prefixes

- `[✓]` — Success
- `[!]` — Warning
- `[✗]` — Error
- `[→]` — Information/Next step
- `[?]` — Question/Prompt

### Example Output

```
╔════════════════════════════════════════╗
║     evmap Installation Script          ║
╚════════════════════════════════════════╝

[→] Checking prerequisites...
[✓] Linux OS detected
[✓] Go 1.26.1 installed

[→] Building evmap...
[✓] Build successful

[→] Installing binary...
[✓] Installed to /usr/local/bin/evmap

[→] Setting up permissions...
[✓] Added user to 'input' group
[✓] Created 'uinput' group
[✓] Added user to 'uinput' group

[→] Configuring system...
[✓] Created udev rule
[✓] Loaded uinput module
[?] Load uinput module on boot? [Y/n]: y
[✓] Configured boot loading

[→] Verifying installation...
[✓] Binary: /usr/local/bin/evmap
[!] Group membership requires logout/login
[✓] /dev/uinput permissions: correct
[✓] uinput module: loaded

╔════════════════════════════════════════╗
║         Installation Complete!         ║
╚════════════════════════════════════════╝

[→] Next steps:
    1. Log out and log back in for group membership
    2. Create config file: ~/.evmap.yaml
    3. Run: evmap

Example config:
─────────────────────────────────────────
log_level: INFO
keymaps:
  - from: up
    to: w
  - from: down
    to: s
─────────────────────────────────────────
```

---

## Behaviours

### INST-01 — Install script detects non-Linux OS
**Given** the script runs on a non-Linux OS  
**When** pre-flight checks execute  
**Then** script exits with error message "This script must run on Linux"

### INST-02 — Install script detects missing Go
**Given** Go is not installed  
**When** pre-flight checks execute  
**Then** script exits with error message and instructions to install Go

### INST-03 — Install script detects old Go version
**Given** Go version is < 1.26  
**When** pre-flight checks execute  
**Then** script exits with error message showing required version

### INST-04 — Build succeeds with clean code
**Given** the repository is clean and Go dependencies are satisfied  
**When** build phase executes  
**Then** `evmap` binary is created in current directory

### INST-05 — Build fails with error
**Given** the code has compilation errors  
**When** build phase executes  
**Then** script exits with error message showing build output

### INST-06 — Binary installs to /usr/local/bin
**Given** build phase succeeded  
**When** installation phase executes  
**Then** `/usr/local/bin/evmap` exists with 755 permissions

### INST-07 — User added to input group
**Given** user is not in input group  
**When** permissions setup executes  
**Then** user is added to input group

### INST-08 — User already in input group
**Given** user is already in input group  
**When** permissions setup executes  
**Then** step is skipped with message "Already in input group"

### INST-09 — uinput group created if missing
**Given** uinput group does not exist  
**When** permissions setup executes  
**Then** uinput group is created

### INST-10 — User added to uinput group
**Given** user is not in uinput group  
**When** permissions setup executes  
**Then** user is added to uinput group

### INST-11 — udev rule created
**Given** `/etc/udev/rules.d/99-uinput.rules` does not exist  
**When** system configuration executes  
**Then** rule file is created with correct content

### INST-12 — udev rule updated if different
**Given** `/etc/udev/rules.d/99-uinput.rules` exists with wrong content  
**When** system configuration executes  
**Then** rule file is updated with correct content

### INST-13 — uinput module loaded
**Given** uinput module is not loaded  
**When** system configuration executes  
**Then** module is loaded via `modprobe`

### INST-14 — Boot loading configured
**Given** user confirms boot loading  
**When** system configuration executes  
**Then** `/etc/modules-load.d/uinput.conf` is created

### INST-15 — Boot loading skipped with flag
**Given** `--no-boot-load` flag provided  
**When** system configuration executes  
**Then** boot loading step is skipped without prompting

### INST-16 — Sample config generated
**Given** `--config` flag provided  
**When** completion phase executes  
**Then** `~/.evmap.yaml` is created with example content

### INST-17 — Script is idempotent
**Given** evmap is already installed  
**When** script runs again  
**Then** all steps check existing state and skip if already complete

### INST-18 — Installation verification succeeds
**Given** all installation steps completed  
**When** verification phase executes  
**Then** binary, permissions, and module are verified correct

---

### UNINST-01 — Uninstall script detects non-Linux OS
**Given** the script runs on a non-Linux OS  
**When** pre-flight checks execute  
**Then** script exits with error message "This script must run on Linux"

### UNINST-02 — User confirms uninstallation
**Given** script runs without `-y` flag  
**When** confirmation prompt appears  
**Then** user must type 'y' or 'yes' to proceed

### UNINST-03 — User declines uninstallation
**Given** script runs without `-y` flag  
**When** user types 'n' at confirmation prompt  
**Then** script exits with message "Uninstall cancelled"

### UNINST-04 — Binary removed from /usr/local/bin
**Given** `/usr/local/bin/evmap` exists  
**When** removal phase executes  
**Then** file is deleted and no longer exists

### UNINST-05 — Binary not found handled gracefully
**Given** `/usr/local/bin/evmap` does not exist  
**When** removal phase executes  
**Then** warning shown and script continues

### UNINST-06 — udev rule removed
**Given** `/etc/udev/rules.d/99-uinput.rules` exists  
**When** cleanup phase executes  
**Then** file is deleted and udev reloaded

### UNINST-07 — Boot loading config removed
**Given** `/etc/modules-load.d/uinput.conf` exists  
**When** cleanup phase executes  
**Then** file is deleted

### UNINST-08 — User removed from groups
**Given** user confirms group removal  
**When** cleanup phase executes  
**Then** user removed from input and uinput groups

### UNINST-09 — Groups kept with flag
**Given** `--keep-groups` flag provided  
**When** cleanup phase executes  
**Then** group removal is skipped without prompting

### UNINST-10 — Config file removal prompted
**Given** `~/.evmap.yaml` exists  
**When** cleanup phase executes  
**Then** user is prompted whether to remove it

### UNINST-11 — Config file kept by default
**Given** user declines config removal  
**When** cleanup phase executes  
**Then** config file is not deleted

### UNINST-12 — Uninstall script is safe to run multiple times
**Given** evmap is already uninstalled  
**When** script runs again  
**Then** all steps handle missing files/groups gracefully

---

## Command-Line Options

### Install Script

| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show usage information |
| `-y`, `--yes` | Skip all prompts (accept defaults) |
| `--no-boot-load` | Skip configuring uinput boot loading |
| `--config` | Generate sample config file at `~/.evmap.yaml` |
| `-v`, `--verbose` | Show detailed output (commands executed) |

### Uninstall Script

| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show usage information |
| `-y`, `--yes` | Skip all prompts and remove everything |
| `--keep-groups` | Do not remove user from input/uinput groups |
| `--keep-config` | Do not remove config file |
| `-v`, `--verbose` | Show detailed output |

---

## Error Handling

### Exit Codes

- `0`: Success
- `1`: General error
- `2`: Missing prerequisites (Go, Linux)
- `3`: Build failed
- `4`: Permission denied (sudo required)
- `5`: User cancelled operation

### Required sudo Operations

The following operations require sudo and should prompt clearly:
- Installing binary to `/usr/local/bin/`
- Creating/modifying udev rules
- Adding user to groups
- Loading kernel modules
- Creating system configuration files

If sudo is not available, script must exit with clear error message.

---

## Testing Strategy

### Install Script Testing

1. **Fresh system test**: Run on clean Linux VM
2. **Already installed test**: Run twice to verify idempotency
3. **Flag combinations**: Test all flags (`-y`, `--no-boot-load`, `--config`)
4. **Error cases**: Test with Go not installed, wrong Go version
5. **Permission cases**: Test without sudo, verify errors
6. **Build failure**: Test with broken code

### Uninstall Script Testing

1. **Full uninstall**: Remove everything after fresh install
2. **Partial uninstall**: Test with missing components
3. **Flag combinations**: Test `--keep-groups`, `--keep-config`
4. **Multiple runs**: Verify safe to run multiple times
5. **Prompt testing**: Test all user confirmation prompts

---

## Future Enhancements (Out of Scope for v1.0)

- Distribution-specific package managers (apt, yum, pacman)
- Systemd service file installation for running as daemon
- Automatic config file generation wizard
- Support for installing from pre-built releases (GitHub)
- Homebrew formula (for Linux)
- Version upgrade path (detect old version, migrate config)
- Rollback functionality (restore previous version)
- Logging of installation steps to file

---

## References

- **README.md**: Installation section (source of truth for manual steps)
- **POSIX shell compatibility**: Scripts should work with bash, dash, zsh
- **Linux FHS**: Follow Filesystem Hierarchy Standard for file locations
- **udev documentation**: https://www.kernel.org/pub/linux/utils/kernel/hotplug/udev/udev.html
