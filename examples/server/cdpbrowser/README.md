# CDP Browser MCP Server

A Chrome DevTools Protocol (CDP) based MCP server that provides browser automation tools.

## Features

- **OS-Compliant Chrome Launching**: Automatically detects and launches Chrome with proper arguments for Linux, macOS, and Windows
- **WebSocket URL Extraction**: Extracts CDP WebSocket URL from Chrome output for reliable connection
- **Chrome Lifecycle Control**: Configurable behavior for Chrome process management
- **MCP Tools**: Navigate, click, screenshot, and control Chrome browser

## Chrome Lifecycle Management

By default, the CDP browser server **keeps Chrome open** when the MCP server exits. This allows users to continue using the browser session even after the automation completes.

### Configuration Options

1. **Environment Variable**: Set `CLOSE_CHROME_ON_EXIT=true` to automatically close Chrome when the server exits
2. **MCP Tool**: Use the `set_chrome_lifecycle` tool to control this behavior at runtime
3. **Manual Control**: Use the `close_browser` tool to explicitly close Chrome when needed

### Available Tools

- `navigate` - Navigate to a URL
- `click` - Click on an element using CSS selectors
- `screenshot` - Take a screenshot of the current page
- `close_browser` - Manually close the Chrome browser
- `set_chrome_lifecycle` - Control whether Chrome stays open when MCP server exits

### Example Usage

```bash
# Keep Chrome open (default behavior)
./cdpbrowser

# Close Chrome when server exits
CLOSE_CHROME_ON_EXIT=true ./cdpbrowser

# Use with mock Chrome for testing
MOCK_CHROME_PATH=./mock_chrome.sh ./cdpbrowser
```

## Chrome Command Detection

The server automatically detects Chrome installation paths:

### Linux
- `/usr/bin/google-chrome-stable`
- `/usr/bin/google-chrome`
- `/usr/bin/chromium-browser`
- `/usr/bin/chromium`

### macOS
- `/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`

### Windows
- `C:\Program Files\Google\Chrome\Application\chrome.exe`
- `C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`

Chrome is launched with remote debugging enabled on port 9222 and appropriate flags for automation use.