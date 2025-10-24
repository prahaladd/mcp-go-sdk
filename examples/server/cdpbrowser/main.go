// CDP browser server with MCP
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	serverName    = "cdpbrowser"
	serverVersion = "1.0.0"
)

type CDPBrowserServer struct {
	ctx            context.Context
	cancel         context.CancelFunc
	allocCtx       context.Context
	allocCancel    context.CancelFunc
	currentURL     string
	chromeCmd      *exec.Cmd
	wsURL          string
	keepChromeOpen bool // Flag to control Chrome lifecycle
}

func NewCDPBrowserServer() *CDPBrowserServer {
	// Check environment variable for Chrome lifecycle control
	keepOpen := true // Default to keeping Chrome open
	if envVal := os.Getenv("CLOSE_CHROME_ON_EXIT"); envVal == "true" || envVal == "1" {
		keepOpen = false
		log.Printf("Environment variable CLOSE_CHROME_ON_EXIT=%s - Chrome will be closed on exit", envVal)
	} else {
		log.Printf("Chrome will remain open when MCP server exits (default behavior)")
	}

	return &CDPBrowserServer{
		keepChromeOpen: keepOpen,
	}
}

// getChromeCommand returns the appropriate Chrome command for the current OS
func getChromeCommand() (string, []string) {
	// Check for mock Chrome path (for testing)
	if mockPath := os.Getenv("MOCK_CHROME_PATH"); mockPath != "" {
		if _, err := os.Stat(mockPath); err == nil {
			return mockPath, []string{} // Mock doesn't need args
		}
	}

	switch runtime.GOOS {
	case "linux":
		// Try different Chrome paths on Linux
		chromePaths := []string{
			"/usr/bin/google-chrome-stable",
			"/usr/bin/google-chrome",
			"/usr/bin/chromium-browser",
			"/usr/bin/chromium",
		}
		for _, path := range chromePaths {
			if _, err := os.Stat(path); err == nil {
				return path, []string{
					"--remote-debugging-port=9222",
					"--no-first-run",
					"--no-default-browser-check",
					"--user-data-dir=/tmp/chrome-remote-profile",
					"--disable-background-timer-throttling",
					"--disable-backgrounding-occluded-windows",
					"--disable-renderer-backgrounding",
					"--disable-features=TranslateUI",
					"--disable-extensions",
					"--no-sandbox",
				}
			}
		}
	case "darwin":
		return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", []string{
			"--remote-debugging-port=9222",
			"--no-first-run",
			"--no-default-browser-check",
			"--user-data-dir=/tmp/chrome-remote-profile",
			"--disable-background-timer-throttling",
			"--disable-backgrounding-occluded-windows",
			"--disable-renderer-backgrounding",
		}
	case "windows":
		// Try different Windows Chrome paths
		chromePaths := []string{
			"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
			"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
		}
		for _, path := range chromePaths {
			if _, err := os.Stat(path); err == nil {
				return path, []string{
					"--remote-debugging-port=9222",
					"--no-first-run",
					"--no-default-browser-check",
					"--user-data-dir=C:\\temp\\chrome-remote-profile",
					"--disable-background-timer-throttling",
					"--disable-backgrounding-occluded-windows",
					"--disable-renderer-backgrounding",
				}
			}
		}
	}

	// Fallback to 'chrome' command in PATH
	return "chrome", []string{
		"--remote-debugging-port=9222",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir=/tmp/chrome-remote-profile",
	}
} // launchChromeAndGetWebSocketURL launches Chrome and extracts the WebSocket URL from output
func (s *CDPBrowserServer) launchChromeAndGetWebSocketURL() error {
	chromePath, args := getChromeCommand()

	log.Printf("Launching Chrome: %s %s", chromePath, strings.Join(args, " "))

	cmd := exec.Command(chromePath, args...)

	// Create pipes to capture stderr (where Chrome outputs the DevTools URL)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %v", err)
	}

	// Start Chrome
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start Chrome: %v", err)
	}

	s.chromeCmd = cmd

	// Read stderr to find the WebSocket URL
	wsURLChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		scanner := bufio.NewScanner(stderr)
		// Regex to match WebSocket URL pattern
		wsPattern := regexp.MustCompile(`DevTools listening on (ws://[^\s]+)`)

		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("Chrome output: %s", line)

			if matches := wsPattern.FindStringSubmatch(line); len(matches) > 1 {
				wsURLChan <- matches[1]
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("error reading Chrome output: %v", err)
		} else {
			errChan <- fmt.Errorf("chrome started but no WebSocket URL found")
		}
	}()

	// Wait for WebSocket URL or timeout
	select {
	case wsURL := <-wsURLChan:
		s.wsURL = wsURL
		log.Printf("Found Chrome WebSocket URL: %s", wsURL)
		return nil
	case err := <-errChan:
		cmd.Process.Kill()
		return err
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		return fmt.Errorf("timeout waiting for Chrome WebSocket URL")
	}
}

// connectToChromeWebSocket connects to Chrome using the extracted WebSocket URL
func (s *CDPBrowserServer) connectToChromeWebSocket() error {
	if s.wsURL == "" {
		return fmt.Errorf("no WebSocket URL available")
	}

	// Create remote allocator with the WebSocket URL
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), s.wsURL)
	s.allocCtx = allocCtx
	s.allocCancel = allocCancel

	// Create context
	ctx, cancel := chromedp.NewContext(allocCtx)
	s.ctx = ctx
	s.cancel = cancel

	// Test the connection
	var title string
	err := chromedp.Run(ctx, chromedp.Title(&title))
	if err != nil {
		s.cleanup()
		return fmt.Errorf("failed to connect to Chrome WebSocket: %v", err)
	}

	log.Printf("Successfully connected to Chrome via WebSocket")
	return nil
}

func (s *CDPBrowserServer) connectToExistingChrome(port int) error {
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(),
		fmt.Sprintf("ws://localhost:%d/", port))
	s.allocCtx = allocCtx
	s.allocCancel = allocCancel

	ctx, cancel := chromedp.NewContext(allocCtx)
	s.ctx = ctx
	s.cancel = cancel

	var title string
	err := chromedp.Run(ctx, chromedp.Title(&title))
	if err != nil {
		s.cleanup()
		return fmt.Errorf("failed to connect to Chrome on port %d: %v", port, err)
	}

	log.Printf("Connected to existing Chrome instance on port %d", port)
	return nil
}

func (s *CDPBrowserServer) launchNewChrome() error {
	// Launch Chrome and get WebSocket URL
	if err := s.launchChromeAndGetWebSocketURL(); err != nil {
		return fmt.Errorf("failed to launch Chrome: %v", err)
	}

	// Connect to Chrome using the WebSocket URL
	if err := s.connectToChromeWebSocket(); err != nil {
		return fmt.Errorf("failed to connect to Chrome: %v", err)
	}

	log.Println("Launched new Chrome instance and connected successfully")
	return nil
}

func (s *CDPBrowserServer) cleanup() {
	// Close CDP connection
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}

	// Only terminate Chrome if explicitly requested
	if s.chromeCmd != nil && s.chromeCmd.Process != nil && !s.keepChromeOpen {
		log.Println("Terminating Chrome process...")
		s.chromeCmd.Process.Kill()
		s.chromeCmd.Wait()
	} else if s.chromeCmd != nil && s.chromeCmd.Process != nil {
		log.Println("Leaving Chrome browser open for continued use...")
		// Detach from the process so it continues running
		s.chromeCmd.Process.Release()
	}
}

func (s *CDPBrowserServer) Initialize() error {
	// Default to launching a new Chrome instance
	log.Println("Launching new Chrome instance...")
	return s.launchNewChrome()
}

type NavigateArgs struct {
	URL string `json:"url" jsonschema:"The URL to navigate to"`
}

func (s *CDPBrowserServer) Navigate(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[NavigateArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	url := req.Params.Arguments.URL
	err := chromedp.Run(s.ctx, chromedp.Navigate(url))
	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error navigating to %s: %v", url, err)},
			},
			IsError: true,
		}, nil
	}

	s.currentURL = url
	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Navigated to %s", url)},
		},
	}, nil
}

type ClickArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector for the element to click"`
}

func (s *CDPBrowserServer) Click(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ClickArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector
	err := chromedp.Run(s.ctx, chromedp.WaitVisible(selector), chromedp.Click(selector))
	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error clicking element %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Clicked element: %s", selector)},
		},
	}, nil
}

func (s *CDPBrowserServer) Screenshot(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[struct{}]]) (*mcp.CallToolResultFor[struct{}], error) {
	var buf []byte
	err := chromedp.Run(s.ctx, chromedp.CaptureScreenshot(&buf))
	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error taking screenshot: %v", err)},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.ImageContent{Data: buf, MIMEType: "image/png"},
		},
	}, nil
}

// CloseBrowser tool - allows user to close Chrome
func (s *CDPBrowserServer) CloseBrowser(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[struct{}]]) (*mcp.CallToolResultFor[struct{}], error) {
	if s.chromeCmd != nil && s.chromeCmd.Process != nil {
		log.Println("User requested to close Chrome browser...")
		s.chromeCmd.Process.Kill()
		s.chromeCmd.Wait()
		s.chromeCmd = nil

		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "Chrome browser closed successfully"},
			},
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "No Chrome process to close"},
		},
	}, nil
}

type ChromeControlArgs struct {
	KeepOpen bool `json:"keep_open" jsonschema:"Whether to keep Chrome open when MCP server exits"`
}

// SetChromeLifecycle tool - allows user to control Chrome lifecycle
func (s *CDPBrowserServer) SetChromeLifecycle(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ChromeControlArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	s.keepChromeOpen = req.Params.Arguments.KeepOpen

	status := "will be closed"
	if s.keepChromeOpen {
		status = "will remain open"
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Chrome lifecycle updated: browser %s when MCP server exits", status)},
		},
	}, nil
}

// ShutdownServer tool - allows graceful server shutdown
func (s *CDPBrowserServer) ShutdownServer(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[struct{}]]) (*mcp.CallToolResultFor[struct{}], error) {
	log.Println("Shutdown requested via MCP tool")

	// Trigger graceful shutdown
	go func() {
		time.Sleep(100 * time.Millisecond) // Give time for response to be sent
		os.Exit(0)
	}()

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "Server shutdown initiated"},
		},
	}, nil
}

func main() {
	log.Printf("Starting %s v%s in long-running mode", serverName, serverVersion)

	server := NewCDPBrowserServer()

	if err := server.Initialize(); err != nil {
		log.Fatalf("Failed to initialize browser: %v", err)
	}
	defer func() {
		log.Println("Shutting down server and cleaning up resources...")
		server.cleanup()
	}()

	log.Println("Browser initialized successfully, ready to accept MCP requests")

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	mcp.AddTool(mcpServer, &mcp.Tool{Name: "navigate", Description: "Navigate to a URL"}, server.Navigate)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "click", Description: "Click on an element"}, server.Click)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "screenshot", Description: "Take a screenshot"}, server.Screenshot)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "close_browser", Description: "Close the Chrome browser"}, server.CloseBrowser)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "set_chrome_lifecycle", Description: "Control whether Chrome stays open when MCP server exits"}, server.SetChromeLifecycle)
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "shutdown_server", Description: "Gracefully shutdown the MCP server"}, server.ShutdownServer)

	transport := &mcp.StdioTransport{}

	log.Println("Server ready - waiting for MCP requests on STDIO")
	if err := mcpServer.Run(context.Background(), transport); err != nil {
		log.Printf("Server stopped with error: %v", err)
	}

	log.Println("Server shutdown complete")
}
