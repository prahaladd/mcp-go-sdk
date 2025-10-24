# CDP Browser Client

A comprehensive MCP client that demonstrates how to interact with the cdpbrowser MCP server. Supports both interactive and script-based automation.

## Prerequisites

Make sure you have Google Chrome installed on your system, as the cdpbrowser server uses Chrome via CDP (Chrome DevTools Protocol).

## Usage

### Build the client:
```bash
cd /home/crazyjarvis/projects/gomcptool/mcp-go-sdk/examples/client/cdpbrowser-client
go mod tidy
go build
```

## Modes of Operation

### 1. Single Command Mode
Execute individual commands:

```bash
# Navigate to a URL
./cdpbrowser-client navigate https://example.com

# Click on an element
./cdpbrowser-client click "button.submit"
./cdpbrowser-client click "#login-button"
./cdpbrowser-client click ".navbar a[href='/about']"

# Take a screenshot
./cdpbrowser-client screenshot

# Set Chrome lifecycle
./cdpbrowser-client lifecycle true   # Keep Chrome open
./cdpbrowser-client lifecycle false  # Close Chrome on exit

# Close browser
./cdpbrowser-client close

# List available tools
./cdpbrowser-client list-tools

# Run demo sequence
./cdpbrowser-client demo
```

### 2. Interactive Mode
Start an interactive session for multiple commands:

```bash
./cdpbrowser-client interactive
```

This starts a persistent session where you can run multiple commands:
```
cdp> navigate https://example.com
cdp> screenshot
cdp> click "a[href='/contact']"
cdp> screenshot
cdp> exit
```

**Interactive Commands:**
- `navigate <url>` - Navigate to a URL
- `click <selector>` - Click on an element using CSS selector
- `screenshot` - Take a screenshot
- `close` - Close browser
- `lifecycle <bool>` - Set Chrome lifecycle
- `list-tools` - List available tools
- `help` - Show available commands
- `exit` or `quit` - Exit interactive mode

### 3. Script Mode (Non-Interactive Automation)
Execute commands from a script file:

```bash
./cdpbrowser-client run-script example-script.txt
```

**Script Format:**
- One command per line
- Comments start with `#`
- Empty lines are ignored
- Same commands as interactive mode, plus:
  - `wait <seconds>` - Wait for specified duration

**Example Script (`example-script.txt`):**
```bash
# Example CDP Browser Script
lifecycle true
navigate https://example.com
wait 2
screenshot
navigate https://github.com
wait 3
screenshot
list-tools
```

## Available Tools

The cdpbrowser server exposes the following tools:

- **navigate**: Navigate to a URL
  - Arguments: `url` (string) - The URL to navigate to

- **click**: Click on an element using CSS selector
  - Arguments: `selector` (string) - CSS selector for the element to click

- **screenshot**: Take a screenshot of the current page
  - No arguments required
  - Screenshots are automatically saved as `screenshot_YYYYMMDD_HHMMSS.png`

- **close_browser**: Close the Chrome browser
  - No arguments required

- **set_chrome_lifecycle**: Control whether Chrome stays open when MCP server exits
  - Arguments: `keep_open` (bool) - Whether to keep Chrome open

## Examples

### Quick Demo
```bash
./cdpbrowser-client demo
```

### Interactive Session
```bash
./cdpbrowser-client interactive
cdp> navigate https://httpbin.org
cdp> screenshot
cdp> click "a[href='/status/200']"
cdp> screenshot
cdp> exit
```

### Script Automation
Create `automation.txt`:
```
# GitHub exploration script
lifecycle true
navigate https://github.com
wait 2
screenshot
navigate https://github.com/explore
wait 2
screenshot
```

Run it:
```bash
./cdpbrowser-client run-script automation.txt
```

## Long-Running Server Architecture

The client automatically:
- Spawns the cdpbrowser server as a subprocess
- Maintains a persistent Chrome instance across commands
- Handles proper cleanup on exit
- Saves all screenshots with timestamps

**Perfect for applications that need to:**
- Read documents and perform browser automation
- Execute sequences of browser actions
- Maintain browser state across multiple operations
- Automate screenshot capture and web interaction

## Notes

- Chrome launches automatically when the server starts
- By default, Chrome remains open after the MCP server exits
- Screenshots are saved in the current directory with timestamp naming
- The server maintains browser state (cookies, navigation history) across commands
- Script mode is ideal for automated testing and document-driven browser automation