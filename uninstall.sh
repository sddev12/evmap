#!/bin/bash
set -e

# evmap Uninstallation Script
# Clean removal of evmap and its system configuration

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
EXIT_PERMISSION=4
EXIT_CANCELLED=5

# Flags
FLAG_YES=0
FLAG_KEEP_GROUPS=0
FLAG_KEEP_CONFIG=0
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

Uninstall evmap and remove system configuration.

OPTIONS:
    -h, --help          Show this help message
    -y, --yes           Skip all prompts (remove everything)
    --keep-groups       Do not remove user from input/uinput groups
    --keep-config       Do not remove config file
    -v, --verbose       Show detailed output

EXAMPLES:
    $0                  Interactive uninstallation
    $0 -y               Non-interactive uninstallation (remove all)
    $0 --keep-groups    Uninstall but keep group memberships
    $0 --keep-config    Uninstall but keep ~/.evmap.yaml

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help) show_help ;;
        -y|--yes) FLAG_YES=1 ;;
        --keep-groups) FLAG_KEEP_GROUPS=1 ;;
        --keep-config) FLAG_KEEP_CONFIG=1 ;;
        -v|--verbose) FLAG_VERBOSE=1; set -x ;;
        *) echo "Unknown option: $1"; show_help ;;
    esac
    shift
done

# Print header
clear
print_header "    evmap Uninstallation Script        "

# Phase 1: Pre-flight Checks
print_info "Checking prerequisites..."

# Check Linux OS
if [ "$(uname -s)" != "Linux" ]; then
    print_error "This script must run on Linux"
    exit $EXIT_PREREQ
fi
print_success "Linux OS detected"

# Confirm uninstallation
echo ""
print_warning "This will remove evmap and its system configuration."
if ! ask_yes_no "Continue with uninstallation?" "n"; then
    print_info "Uninstall cancelled"
    exit $EXIT_CANCELLED
fi

# Phase 2: Remove Binary
echo ""
print_info "Removing binary..."

if [ -f "/usr/local/bin/evmap" ]; then
    if sudo rm -f /usr/local/bin/evmap; then
        print_success "Removed /usr/local/bin/evmap"
    else
        print_error "Failed to remove binary (sudo required)"
        exit $EXIT_PERMISSION
    fi
else
    print_warning "Binary not found at /usr/local/bin/evmap"
fi

# Phase 3: Remove System Configuration
echo ""
print_info "Removing system configuration..."

# Remove udev rule
UDEV_FILE="/etc/udev/rules.d/99-uinput.rules"
if [ -f "$UDEV_FILE" ]; then
    if sudo rm -f "$UDEV_FILE"; then
        print_success "Removed udev rule"
        # Reload udev
        sudo udevadm control --reload-rules 2>/dev/null || true
        sudo udevadm trigger 2>/dev/null || true
    else
        print_error "Failed to remove udev rule"
    fi
else
    print_success "udev rule not found (already removed)"
fi

# Remove boot loading config
BOOT_CONF="/etc/modules-load.d/uinput.conf"
if [ -f "$BOOT_CONF" ]; then
    if sudo rm -f "$BOOT_CONF"; then
        print_success "Removed boot loading config"
    else
        print_warning "Failed to remove boot loading config"
    fi
else
    print_success "Boot loading config not found (already removed)"
fi

# Unload uinput module (optional)
if lsmod | grep -q "^uinput"; then
    if ask_yes_no "Unload uinput module now?" "n"; then
        if sudo modprobe -r uinput 2>/dev/null; then
            print_success "Unloaded uinput module"
        else
            print_warning "Could not unload uinput module (may be in use)"
        fi
    else
        print_info "Skipped module unloading"
    fi
else
    print_success "uinput module not loaded"
fi

# Phase 4: Group Cleanup
if [ "$FLAG_KEEP_GROUPS" -eq 0 ]; then
    echo ""
    print_info "Group cleanup..."
    
    if ask_yes_no "Remove user from input and uinput groups?" "n"; then
        # Remove from input group
        if groups "$USER" | grep -q "\binput\b"; then
            if sudo gpasswd -d "$USER" input 2>/dev/null; then
                print_success "Removed user from 'input' group"
            else
                print_warning "Failed to remove user from 'input' group"
            fi
        else
            print_success "User not in 'input' group"
        fi
        
        # Remove from uinput group
        if groups "$USER" | grep -q "\buinput\b"; then
            if sudo gpasswd -d "$USER" uinput 2>/dev/null; then
                print_success "Removed user from 'uinput' group"
            else
                print_warning "Failed to remove user from 'uinput' group"
            fi
        else
            print_success "User not in 'uinput' group"
        fi
        
        # Check if uinput group has other members
        UINPUT_MEMBERS=$(getent group uinput | cut -d: -f4)
        if [ -z "$UINPUT_MEMBERS" ]; then
            if ask_yes_no "Delete empty 'uinput' group?" "n"; then
                if sudo groupdel uinput 2>/dev/null; then
                    print_success "Deleted 'uinput' group"
                else
                    print_warning "Could not delete 'uinput' group"
                fi
            else
                print_info "Kept 'uinput' group"
            fi
        else
            print_info "uinput group has other members, not deleting"
        fi
    else
        print_info "Skipped group removal (user still in input/uinput groups)"
    fi
else
    echo ""
    print_info "Skipped group removal (--keep-groups flag)"
fi

# Phase 5: Config File Cleanup
if [ "$FLAG_KEEP_CONFIG" -eq 0 ]; then
    echo ""
    print_info "Config file cleanup..."
    
    CONFIG_FILE="$HOME/.evmap.yaml"
    if [ -f "$CONFIG_FILE" ]; then
        if ask_yes_no "Remove config file ~/.evmap.yaml?" "n"; then
            if rm -f "$CONFIG_FILE"; then
                print_success "Removed $CONFIG_FILE"
            else
                print_warning "Failed to remove config file"
            fi
        else
            print_info "Kept config file"
        fi
    else
        print_success "No config file found"
    fi
    
    # Check for config in current directory
    if [ -f ".evmap.yaml" ]; then
        print_info "Found .evmap.yaml in current directory"
        if ask_yes_no "Remove .evmap.yaml from current directory?" "n"; then
            if rm -f ".evmap.yaml"; then
                print_success "Removed .evmap.yaml from current directory"
            else
                print_warning "Failed to remove .evmap.yaml"
            fi
        else
            print_info "Kept .evmap.yaml in current directory"
        fi
    fi
else
    echo ""
    print_info "Skipped config file removal (--keep-config flag)"
fi

# Phase 6: Completion
echo ""
print_header "       Uninstallation Complete!        "

echo ""
print_info "Summary:"
echo "    • Binary removed from /usr/local/bin/"
echo "    • System configuration cleaned up"

if [ "$FLAG_KEEP_GROUPS" -eq 0 ]; then
    if groups "$USER" | grep -qE "\b(input|uinput)\b"; then
        echo "    • Group memberships changed (logout required)"
    fi
fi

if [ "$FLAG_KEEP_CONFIG" -eq 1 ]; then
    echo "    • Config file kept (use --keep-config=false to remove)"
fi

echo ""
print_info "evmap has been uninstalled"

if [ "$FLAG_KEEP_GROUPS" -eq 0 ]; then
    if ! groups "$USER" | grep -qE "\b(input|uinput)\b"; then
        echo ""
        print_warning "Log out and log back in for group changes to take effect"
    fi
fi

echo ""

exit $EXIT_SUCCESS
