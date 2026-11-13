#!/bin/bash

set -euo pipefail

# CyberStrikeAI one-click deploy and start script
ROOT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT_DIR"

# Color definitions
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Print colored messages
info() { echo -e "${BLUE}ℹ️  $1${NC}"; }
success() { echo -e "${GREEN}✅ $1${NC}"; }
warning() { echo -e "${YELLOW}⚠️  $1${NC}"; }
error() { echo -e "${RED}❌ $1${NC}"; }
note() { echo -e "${CYAN}ℹ️  $1${NC}"; }

# Temporary mirror/proxy settings (only effective in this script)
PIP_INDEX_URL="${PIP_INDEX_URL:-https://pypi.tuna.tsinghua.edu.cn/simple}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

# Save original env vars (for restoration)
ORIGINAL_PIP_INDEX_URL="${PIP_INDEX_URL:-}"
ORIGINAL_GOPROXY="${GOPROXY:-}"

# Progress display helper
show_progress() {
    local pid=$1
    local message=$2
    local i=0
    local dots=""
    
    # Check if the process exists
    if ! kill -0 "$pid" 2>/dev/null; then
        # Process already finished; return immediately
        return 0
    fi
    
    while kill -0 "$pid" 2>/dev/null; do
        i=$((i + 1))
        case $((i % 4)) in
            0) dots="." ;;
            1) dots=".." ;;
            2) dots="..." ;;
            3) dots="...." ;;
        esac
        printf "\r${BLUE}⏳ %s%s${NC}" "$message" "$dots"
        sleep 0.5
        
        # Re-check whether the process is still running
        if ! kill -0 "$pid" 2>/dev/null; then
            break
        fi
    done
    printf "\r"
}

print_banner() {
    local show_mirrors="${1:-1}"
    echo ""
    echo "=========================================="
    echo "  CyberStrikeAI Deploy & Start Script"
    echo "  (HTTPS with self-signed cert by default; plain HTTP: $0 --http)"
    echo "=========================================="
    echo ""

    if [ "$show_mirrors" -eq 1 ]; then
        # Show temporary mirror/proxy info
        echo ""
        warning "Note: this script uses temporary mirrors to speed up downloads"
        echo ""
        info "Python pip temporary mirror:"
        echo "  ${PIP_INDEX_URL}"
        info "Go temporary proxy:"
        echo "  ${GOPROXY}"
        echo ""
        note "These settings apply only while this script runs and do not change system config"
        echo ""
        sleep 1
    fi
}

CONFIG_FILE="$ROOT_DIR/config.yaml"
EXAMPLE_CONFIG_FILE="$ROOT_DIR/config.example.yaml"
VENV_DIR="$ROOT_DIR/venv"
REQUIREMENTS_FILE="$ROOT_DIR/requirements.txt"
BINARY_NAME="cyberstrike-ai"

# Check Python environment
check_python() {
    if ! command -v python3 >/dev/null 2>&1; then
        error "python3 not found"
        echo ""
        info "Install Python 3.10 or later first:"
        echo "  macOS:   brew install python3"
        echo "  Ubuntu:  sudo apt-get install python3 python3-venv"
        echo "  CentOS:  sudo yum install python3 python3-pip"
        exit 1
    fi
    
    PYTHON_VERSION=$(python3 --version 2>&1 | awk '{print $2}')
    PYTHON_MAJOR=$(echo "$PYTHON_VERSION" | cut -d. -f1)
    PYTHON_MINOR=$(echo "$PYTHON_VERSION" | cut -d. -f2)
    
    if [ "$PYTHON_MAJOR" -lt 3 ] || ([ "$PYTHON_MAJOR" -eq 3 ] && [ "$PYTHON_MINOR" -lt 10 ]); then
        error "Python version too old: $PYTHON_VERSION (requires 3.10+)"
        exit 1
    fi
    
    success "Python check passed: $PYTHON_VERSION"
}

# Check Go environment
check_go() {
    if ! command -v go >/dev/null 2>&1; then
        error "Go not found"
        echo ""
        info "Install Go 1.21 or later first:"
        echo "  macOS:   brew install go"
        echo "  Ubuntu:  sudo apt-get install golang-go"
        echo "  CentOS:  sudo yum install golang"
        echo "  Or visit: https://go.dev/dl/"
        exit 1
    fi
    
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)
    
    if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 21 ]); then
        error "Go version too old: $GO_VERSION (requires 1.21+)"
        exit 1
    fi
    
    success "Go check passed: $(go version)"
}

check_go_quiet() {
    if ! command -v go >/dev/null 2>&1; then
        error "Go not found"
        echo ""
        info "Install Go 1.21 or later first:"
        echo "  macOS:   brew install go"
        echo "  Ubuntu:  sudo apt-get install golang-go"
        echo "  CentOS:  sudo yum install golang"
        echo "  Or visit: https://go.dev/dl/"
        exit 1
    fi

    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)

    if [ "$GO_MAJOR" -lt 1 ] || ([ "$GO_MAJOR" -eq 1 ] && [ "$GO_MINOR" -lt 21 ]); then
        error "Go version too old: $GO_VERSION (requires 1.21+)"
        exit 1
    fi
}

# Set up Python virtual environment
setup_python_env() {
    if [ ! -d "$VENV_DIR" ]; then
        info "Creating Python virtual environment..."
        python3 -m venv "$VENV_DIR"
        success "Virtual environment created"
    else
        info "Python virtual environment already exists"
    fi
    
    info "Activating virtual environment..."
    # shellcheck disable=SC1091
    source "$VENV_DIR/bin/activate"
    
    if [ -f "$REQUIREMENTS_FILE" ]; then
        echo ""
        note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        note "Using temporary pip mirror (this script run only)"
        note "   Mirror URL: ${PIP_INDEX_URL}"
        note "   For a permanent setting, set the PIP_INDEX_URL env var"
        note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        
        info "Upgrading pip..."
        pip install --index-url "$PIP_INDEX_URL" --upgrade pip >/dev/null 2>&1 || true
        
        info "Installing Python dependencies..."
        echo ""
        
        # Install deps in background; capture errors and show progress
        PIP_LOG=$(mktemp)
        (
            set +e  # disable errexit in subshell
            pip install --index-url "$PIP_INDEX_URL" -r "$REQUIREMENTS_FILE" >"$PIP_LOG" 2>&1
            echo $? > "${PIP_LOG}.exit"
        ) &
        PIP_PID=$!
        
        # Brief pause so the process can start
        sleep 0.1
        
        # Show progress while still running
        if kill -0 "$PIP_PID" 2>/dev/null; then
            show_progress "$PIP_PID" "Installing dependencies"
        else
            # Process already finished; wait for exit code file
            sleep 0.2
        fi
        
        # Wait for completion; ignore wait exit code
        wait "$PIP_PID" 2>/dev/null || true
        
        PIP_EXIT_CODE=0
        if [ -f "${PIP_LOG}.exit" ]; then
            PIP_EXIT_CODE=$(cat "${PIP_LOG}.exit" 2>/dev/null || echo "1")
            rm -f "${PIP_LOG}.exit" 2>/dev/null || true
        else
            # No exit code file; check log for errors
            if [ -f "$PIP_LOG" ] && grep -q -i "error\|failed\|exception" "$PIP_LOG" 2>/dev/null; then
                PIP_EXIT_CODE=1
            fi
        fi
        
        if [ $PIP_EXIT_CODE -eq 0 ]; then
            success "Python dependencies installed"
        else
            # Check for angr install failure (needs Rust)
            if grep -q "angr" "$PIP_LOG" && grep -q "Rust compiler\|can't find Rust" "$PIP_LOG"; then
                warning "angr install failed (Rust compiler required)"
                echo ""
                info "angr is optional and mainly used for binary analysis tools"
                info "To use angr, install Rust first:"
                echo "  macOS:   curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
                echo "  Ubuntu:  curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
                echo "  Or visit: https://rustup.rs/"
                echo ""
                info "Other dependencies are installed; you can continue (some tools may be unavailable)"
            else
                warning "Some Python dependencies failed to install, but continuing"
                warning "If you hit issues, check the errors and install missing packages manually"
                # Show last lines of error output
                echo ""
                info "Error details (last 10 lines):"
                tail -n 10 "$PIP_LOG" | sed 's/^/  /'
                echo ""
            fi
        fi
        rm -f "$PIP_LOG"
    else
        warning "requirements.txt not found; skipping Python dependency install"
    fi
}

# Build Go project
build_go_project() {
    echo ""
    note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    note "Using temporary Go proxy (this script run only)"
    note "   Proxy URL: ${GOPROXY}"
    note "   For a permanent setting, set the GOPROXY env var"
    note "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    
    info "Downloading Go dependencies..."
    GO_DOWNLOAD_LOG=$(mktemp)
    (
        set +e  # disable errexit in subshell
        export GOPROXY="$GOPROXY"
        go mod download >"$GO_DOWNLOAD_LOG" 2>&1
        echo $? > "${GO_DOWNLOAD_LOG}.exit"
    ) &
    GO_DOWNLOAD_PID=$!
    
    # Brief pause so the process can start
    sleep 0.1
    
    # Show progress while still running
    if kill -0 "$GO_DOWNLOAD_PID" 2>/dev/null; then
        show_progress "$GO_DOWNLOAD_PID" "Downloading Go dependencies"
    else
        # Process already finished; wait for exit code file
        sleep 0.2
    fi
    
    # Wait for completion; ignore wait exit code
    wait "$GO_DOWNLOAD_PID" 2>/dev/null || true
    
    GO_DOWNLOAD_EXIT_CODE=0
    if [ -f "${GO_DOWNLOAD_LOG}.exit" ]; then
        GO_DOWNLOAD_EXIT_CODE=$(cat "${GO_DOWNLOAD_LOG}.exit" 2>/dev/null || echo "1")
        rm -f "${GO_DOWNLOAD_LOG}.exit" 2>/dev/null || true
    else
        # No exit code file; check log for errors
        if [ -f "$GO_DOWNLOAD_LOG" ] && grep -q -i "error\|failed" "$GO_DOWNLOAD_LOG" 2>/dev/null; then
            GO_DOWNLOAD_EXIT_CODE=1
        fi
    fi
    rm -f "$GO_DOWNLOAD_LOG" 2>/dev/null || true
    
    if [ $GO_DOWNLOAD_EXIT_CODE -ne 0 ]; then
        error "Go dependency download failed"
        exit 1
    fi
    success "Go dependencies downloaded"
    
    info "Building project..."
    GO_BUILD_LOG=$(mktemp)
    (
        set +e  # disable errexit in subshell
        export GOPROXY="$GOPROXY"
        go build -o "$BINARY_NAME" ./cmd/server >"$GO_BUILD_LOG" 2>&1
        echo $? > "${GO_BUILD_LOG}.exit"
    ) &
    GO_BUILD_PID=$!
    
    # Brief pause so the process can start
    sleep 0.1
    
    # Show progress while still running
    if kill -0 "$GO_BUILD_PID" 2>/dev/null; then
        show_progress "$GO_BUILD_PID" "Building project"
    else
        # Process already finished; wait for exit code file
        sleep 0.2
    fi
    
    # Wait for completion; ignore wait exit code
    wait "$GO_BUILD_PID" 2>/dev/null || true
    
    GO_BUILD_EXIT_CODE=0
    if [ -f "${GO_BUILD_LOG}.exit" ]; then
        GO_BUILD_EXIT_CODE=$(cat "${GO_BUILD_LOG}.exit" 2>/dev/null || echo "1")
        rm -f "${GO_BUILD_LOG}.exit" 2>/dev/null || true
    else
        # No exit code file; check log for errors
        if [ -f "$GO_BUILD_LOG" ] && grep -q -i "error\|failed" "$GO_BUILD_LOG" 2>/dev/null; then
            GO_BUILD_EXIT_CODE=1
        fi
    fi
    
    if [ $GO_BUILD_EXIT_CODE -eq 0 ]; then
        success "Build complete: $BINARY_NAME"
        rm -f "$GO_BUILD_LOG"
    else
        error "Build failed"
        # Show build errors
        echo ""
        info "Build error details:"
        cat "$GO_BUILD_LOG" | sed 's/^/  /'
        echo ""
        rm -f "$GO_BUILD_LOG"
        exit 1
    fi
}

build_go_project_quiet() {
    info "Building $BINARY_NAME..."

    GO_DOWNLOAD_LOG=$(mktemp)
    if ! GOPROXY="$GOPROXY" go mod download >"$GO_DOWNLOAD_LOG" 2>&1; then
        error "Go dependency download failed"
        echo ""
        info "Download error details:"
        cat "$GO_DOWNLOAD_LOG" | sed 's/^/  /'
        echo ""
        rm -f "$GO_DOWNLOAD_LOG"
        exit 1
    fi
    rm -f "$GO_DOWNLOAD_LOG"

    GO_BUILD_LOG=$(mktemp)
    if ! GOPROXY="$GOPROXY" go build -o "$BINARY_NAME" ./cmd/server >"$GO_BUILD_LOG" 2>&1; then
        error "Build failed"
        echo ""
        info "Build error details:"
        cat "$GO_BUILD_LOG" | sed 's/^/  /'
        echo ""
        rm -f "$GO_BUILD_LOG"
        exit 1
    fi
    rm -f "$GO_BUILD_LOG"
}

# Check whether a rebuild is needed
need_rebuild() {
    if [ ! -f "$BINARY_NAME" ]; then
        return 0  # needs build
    fi
    
    # Check if source changed since last build
    if [ "$BINARY_NAME" -ot cmd/server/main.go ] || \
       [ "$BINARY_NAME" -ot go.mod ] || \
       find internal cmd -name "*.go" -newer "$BINARY_NAME" 2>/dev/null | grep -q .; then
        return 0  # needs rebuild
    fi
    
    return 1  # no rebuild needed
}

# Main flow
# Default: HTTPS (--https passed to binary); --http forces plain HTTP even if config.yaml enables TLS.
main() {
    USE_HTTPS=1
    RESET_ADMIN_PASSWORD=0
    FORWARD_ARGS=()
    for arg in "$@"; do
        if [ "$arg" = "--http" ]; then
            USE_HTTPS=0
            continue
        fi
        if [ "$arg" = "--https" ]; then
            USE_HTTPS=1
            continue
        fi
        if [ "$arg" = "--reset-admin-password" ]; then
            RESET_ADMIN_PASSWORD=1
            continue
        fi
        FORWARD_ARGS+=("$arg")
    done

    if [ "$RESET_ADMIN_PASSWORD" -eq 1 ]; then
        if [ ! -f "$CONFIG_FILE" ] && [ ! -f "$EXAMPLE_CONFIG_FILE" ]; then
            error "config.yaml not found, and config.example.yaml is missing"
            info "The server binary creates config.yaml from config.example.yaml on first start"
            exit 1
        fi
        check_go_quiet
        if need_rebuild; then
            build_go_project_quiet
            echo ""
        fi
        if [ "${#FORWARD_ARGS[@]}" -gt 0 ]; then
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --reset-admin-password "${FORWARD_ARGS[@]}"
        else
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --reset-admin-password
        fi
    fi

    print_banner 1

    # Environment checks
    info "Checking runtime environment..."
    check_python
    if [ ! -f "$CONFIG_FILE" ] && [ ! -f "$EXAMPLE_CONFIG_FILE" ]; then
        error "config.yaml not found, and config.example.yaml is missing"
        info "The server binary creates config.yaml from config.example.yaml on first start"
        exit 1
    fi
    check_go
    info "Preparing Pi agent runtime..."
    bash "$ROOT_DIR/scripts/build-pi-runtime.sh"
    echo ""
    
    # Python setup
    info "Setting up Python environment..."
    setup_python_env
    echo ""
    
    # Go build
    if need_rebuild; then
        info "Preparing to build project..."
        build_go_project
    else
        success "Binary is up to date; skipping build"
    fi
    echo ""
    
    # Start server
    success "All setup complete!"
    echo ""
    if [ "$USE_HTTPS" -eq 1 ]; then
        info "Starting CyberStrikeAI server (HTTPS + HTTP/2, self-signed cert)..."
        note "For plain HTTP, use: $0 --http"
    else
        info "Starting CyberStrikeAI server (HTTP)..."
    fi
    echo "=========================================="
    echo ""

    # Always pass config.yaml from project root so cwd does not matter; extra args still apply (e.g. -config override; last Go flag wins).
    if [ "$USE_HTTPS" -eq 1 ]; then
        if [ "${#FORWARD_ARGS[@]}" -gt 0 ]; then
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --https "${FORWARD_ARGS[@]}"
        else
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --https
        fi
    else
        if [ "${#FORWARD_ARGS[@]}" -gt 0 ]; then
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --http "${FORWARD_ARGS[@]}"
        else
            exec "./$BINARY_NAME" -config "$CONFIG_FILE" --http
        fi
    fi
}

# Run main (supports args, e.g. ./run.sh --http, ./run.sh --reset-admin-password)
main "$@"
