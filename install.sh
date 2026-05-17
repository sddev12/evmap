#!/bin/bash
set -e

# evmap Installation Script
# Automates build, installation, and system configuration

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Message prefixes
SUCCESS="[✓]"
WARNING="[!]"
ERROR="[✗]"
INFO="[→]"
PROMPT="[?]"

# Exit codes
EXIT_SUCCESS=0
EXIT_ERROR=1
EXIT_PREREQ=2
EXIT_BUILD=3
EXIT_PERMISSION=4
EXIT_CANCELLED=5

# Flags
FLAG_YES=0
FLAG_NO_BOOT_LOAD=0
FLAG_CONFIG=0
FLAG_VERBOSE=0

# Helper functions
print_header() {
    echo ""
    echo -e "${BLUE}╔════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║${NC}     $1${BLUE}║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════╝${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}${SUCCESS}${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}${WARNING}${NC} $1"
}

print_error() {
    echo -e "${RED}${ERROR}${NC} $1"
}

print_info() {
    echo -e "${CYAN}${INFO}${NC} $1"
}

print_prompt() {
    echo -e "${YELLOW}${PROMPT}${NC} $1"
}

ask_yes_no() {
    if [ "$FLAG_YES" -eq 1 ]; then
        return 0
    fi
    local prompt="$1"
    local default="${2:-n}"
    
    if [ "$default" = "y" ]; then
        prompt="$prompt [Y/n]: "
    else
        prompt="$prompt [y/N]: "
    fi
    
    read -p "$(echo -e "${YELLOW}${PROMPT}${NC} $prompt")" response
    response=${response:-$default}
    
    case "$response" in
        [yY][eE][sS]|[yY]) return 0 ;;
        *) return 1 ;;
    esac
}

# Parse command-line arguments
show_help() {
    cat << EOF
Usage: $0 [OPTIONS]

Install evmap with automated system configuration.

OPTIONS:
    -h, --help          Show this help message
    -y, --yes           Skip all prompts (accept defaults)
    --no-boot-load      Skip configuring uinput boot loading
    --config            Generate sample config file at ~/.evmap.yaml
    -v, --verbose       Show detailed output

EXAMPLES:
    $0                  Interactive installation
    $0 -y               Non-interactive installation
    $0 --config         Install and generate sample config
    $0 --no-boot-load   Install without boot loading setup

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help) show_help ;;
        -y|--yes) FLAG_YES=1 ;;
        --no-boot-load) FLAG_NO_BOOT_LOAD=1 ;;
        --config) FLAG_CONFIG=1 ;;
        -v|--verbose) FLAG_VERBOSE=1; set -x ;;
        *) echo "Unknown option: $1"; show_help ;;
    esac
    shift
done

# Print header
clear
print_header "     evmap Installation Script          "

# Phase 1: Pre-flight Checks
print_info "Checking prerequisites..."

# Check Linux OS
if [ "$(uname -s)" != "Linux" ]; then
    print_error "This script must run on Linux"
    exit $EXIT_PREREQ
fi
print_success "Linux OS detected"

# Check Go installation
if ! command -v go &> /dev/null; then
    print_error "Go is not installed"
    echo ""
    echo "Please install Go 1.26 or later from https://go.dev/dl/"
    exit $EXIT_PREREQ
fi

GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)

if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 26 ]); then
    print_error "Go version $GO_VERSION is too old (require 1.26+)"
    echo ""
    echo "Please upgrade Go from https://go.dev/dl/"
    exit $EXIT_PREREQ
fi
print_success "Go $GO_VERSION installed"

# Check if already installed
if [ -f "/usr/local/bin/evmap" ]; then
    print_warning "evmap is already installed at /usr/local/bin/evmap"
    if ask_yes_no "Reinstall/upgrade?" "y"; then
        print_info "Proceeding with reinstallation..."
    else
        print_info "Installation cancelled"
        exit $EXIT_CANCELLED
    fi
fi

# Phase 2: Build
echo ""
print_info "Building evmap..."

# Check if binary already exists
if [ -f "./evmap" ]; then
    print_warning "Binary already exists in current directory"
    if ask_yes_no "Rebuild?" "y"; then
        rm -f ./evmap
    fi
fi

# Build binary
if [ ! -f "./evmap" ]; then
    if ! go build -o evmap .; then
        print_error "Build failed"
        exit $EXIT_BUILD
    fi
fi

# Verify binary exists
if [ ! -f "./evmap" ]; then
    print_error "Binary not found after build"
    exit $EXIT_BUILD
fi

print_success "Build successful"

# Phase 3: Installation
echo ""
print_info "Installing binary..."

# Install binary (requires sudo)
if ! sudo install -o root -g root -m 0755 ./evmap /usr/local/bin/evmap; then
    print_error "Failed to install binary (sudo required)"
    exit $EXIT_PERMISSION
fi

# Verify installation
if [ ! -f "/usr/local/bin/evmap" ]; then
    print_error "Binary not found at /usr/local/bin/evmap after installation"
    exit $EXIT_ERROR
fi

print_success "Installed to /usr/local/bin/evmap"

# Check PATH
if ! echo "$PATH" | grep -q "/usr/local/bin"; then
    print_warning "/usr/local/bin is not in your PATH"
    echo "          Add it to your shell profile: export PATH=\"/usr/local/bin:\$PATH\""
fi

# Phase 4: Permissions Setup
echo ""
print_info "Setting up permissions..."

# Add user to input group
if groups "$USER" | grep -q "\binput\b"; then
    print_success "User already in 'input' group"
else
    if sudo usermod -a -G input "$USER"; then
        print_success "Added user to 'input' group"
    else
        print_error "Failed to add user to 'input' group"
        exit $EXIT_PERMISSION
    fi
fi

# Create uinput group
if getent group uinput > /dev/null 2>&1; then
    print_success "'uinput' group already exists"
else
    if sudo groupadd uinput; then
        print_success "Created 'uinput' group"
    else
        print_error "Failed to create 'uinput' group"
        exit $EXIT_PERMISSION
    fi
fi

# Add user to uinput group
if groups "$USER" | grep -q "\buinput\b"; then
    print_success "User already in 'uinput' group"
else
    if sudo usermod -a -G uinput "$USER"; then
        print_success "Added user to 'uinput' group"
    else
        print_error "Failed to add user to 'uinput' group"
        exit $EXIT_PERMISSION
    fi
fi

# Phase 5: System Configuration
echo ""
print_info "Configuring system..."

# Create udev rule
UDEV_RULE='KERNEL=="uinput", GROUP="uinput", MODE="0660"'
UDEV_FILE="/etc/udev/rules.d/99-uinput.rules"

if [ -f "$UDEV_FILE" ]; then
    EXISTING_RULE=$(cat "$UDEV_FILE")
    if [ "$EXISTING_RULE" = "$UDEV_RULE" ]; then
        print_success "udev rule already configured"
    else
        print_warning "udev rule exists but differs, updating..."
        echo "$UDEV_RULE" | sudo tee "$UDEV_FILE" > /dev/null
        print_success "Updated udev rule"
    fi
else
    echo "$UDEV_RULE" | sudo tee "$UDEV_FILE" > /dev/null
    print_success "Created udev rule"
fi

# Reload udev
sudo udevadm control --reload-rules 2>/dev/null || true
sudo udevadm trigger 2>/dev/null || true

# Load uinput module
if lsmod | grep -q "^uinput"; then
    print_success "uinput module already loaded"
else
    if sudo modprobe uinput; then
        print_success "Loaded uinput module"
    else
        print_warning "Failed to load uinput module (may require reboot)"
    fi
fi

# Configure boot loading
if [ "$FLAG_NO_BOOT_LOAD" -eq 0 ]; then
    if [ -f "/etc/modules-load.d/uinput.conf" ]; then
        print_success "uinput boot loading already configured"
    else
        if ask_yes_no "Configure uinput to load on boot?" "y"; then
            echo "uinput" | sudo tee /etc/modules-load.d/uinput.conf > /dev/null
            print_success "Configured boot loading"
        else
            print_info "Skipped boot loading configuration"
        fi
    fi
else
    print_info "Skipped boot loading configuration (--no-boot-load)"
fi

# Phase 6: Verification
echo ""
print_info "Verifying installation..."

# Verify binary
if command -v evmap > /dev/null 2>&1; then
    print_success "Binary: $(which evmap)"
else
    print_error "Binary not found in PATH"
fi

# Verify group membership
if groups "$USER" | grep -q "\binput\b" && groups "$USER" | grep -q "\buinput\b"; then
    print_warning "Group membership requires logout/login to take effect"
else
    print_error "User not in required groups"
fi

# Verify /dev/uinput permissions
if [ -e "/dev/uinput" ]; then
    UINPUT_PERMS=$(ls -l /dev/uinput 2>/dev/null | awk '{print $1, $3, $4}')
    if echo "$UINPUT_PERMS" | grep -q "uinput"; then
        print_success "/dev/uinput permissions: correct"
    else
        print_warning "/dev/uinput permissions may need adjustment"
    fi
else
    print_warning "/dev/uinput not found (may require reboot or module load)"
fi

# Verify uinput module
if lsmod | grep -q "^uinput"; then
    print_success "uinput module: loaded"
else
    print_warning "uinput module: not loaded"
fi

# Phase 7: Completion
echo ""
print_header "         Installation Complete!         "

print_info "Next steps:"
echo "    1. Log out and log back in for group membership"
echo "    2. Create config file: ~/.evmap.yaml"
echo "    3. Run: evmap"
echo ""

# Generate sample config if requested
if [ "$FLAG_CONFIG" -eq 1 ]; then
    CONFIG_FILE="$HOME/.evmap.yaml"
    if [ -f "$CONFIG_FILE" ]; then
        print_warning "Config file already exists at $CONFIG_FILE"
        if ask_yes_no "Overwrite?" "n"; then
            cat > "$CONFIG_FILE" << 'EOF'
log_level: INFO

# Optional: Only remap when this window is in focus
# focus:
#   window_title: "Hearts of Iron IV"

# Remap arrow keys to WASD
keymaps:
  - from: up
    to: w
  - from: down
    to: s
  - from: left
    to: a
  - from: right
    to: d
EOF
            print_success "Sample config generated at $CONFIG_FILE"
        fi
    else
        cat > "$CONFIG_FILE" << 'EOF'
log_level: INFO

# Optional: Only remap when this window is in focus
# focus:
#   window_title: "Hearts of Iron IV"

# Remap arrow keys to WASD
keymaps:
  - from: up
    to: w
  - from: down
    to: s
  - from: left
    to: a
  - from: right
    to: d
EOF
        print_success "Sample config generated at $CONFIG_FILE"
    fi
    echo ""
fi

echo "Example config:"
echo "─────────────────────────────────────────"
echo "log_level: INFO"
echo ""
echo "keymaps:"
echo "  - from: up"
echo "    to: w"
echo "  - from: down"
echo "    to: s"
echo "  - from: left"
echo "    to: a"
echo "  - from: right"
echo "    to: d"
echo "─────────────────────────────────────────"
echo ""

exit $EXIT_SUCCESS
