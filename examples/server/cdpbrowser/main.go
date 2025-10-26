// CDP browser server with MCP
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/cdp"
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
	chromePort     int  // Random port for this instance
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

	// Generate random port between 9222-9322 to avoid conflicts
	rand.Seed(time.Now().UnixNano())
	port := 9222 + rand.Intn(100)

	return &CDPBrowserServer{
		keepChromeOpen: keepOpen,
		chromePort:     port,
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
	log.Printf("Attempting to connect to Chrome WebSocket: %s", s.wsURL)

	if s.wsURL == "" {
		return fmt.Errorf("no WebSocket URL available")
	}

	log.Println("Creating remote allocator with WebSocket URL...")
	// Create remote allocator with the WebSocket URL
	allocCtx, allocCancel := chromedp.NewRemoteAllocator(context.Background(), s.wsURL)
	s.allocCtx = allocCtx
	s.allocCancel = allocCancel

	log.Println("Creating Chrome context...")
	// Create context
	ctx, cancel := chromedp.NewContext(allocCtx)
	s.ctx = ctx
	s.cancel = cancel

	log.Println("Testing Chrome connection by getting page title...")
	// Test the connection
	var title string
	err := chromedp.Run(ctx, chromedp.Title(&title))
	if err != nil {
		log.Printf("Failed to get page title, cleaning up: %v", err)
		s.cleanup()
		return fmt.Errorf("failed to connect to Chrome WebSocket: %v", err)
	}

	log.Printf("Successfully connected to Chrome via WebSocket - page title: '%s'", title)
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

// killExistingChromeProcesses kills any existing Chrome processes to avoid conflicts
func (s *CDPBrowserServer) killExistingChromeProcesses() {
	log.Println("Killing any existing Chrome processes to avoid conflicts...")

	// Try to kill Chrome processes on the default debugging port
	exec.Command("pkill", "-f", "chrome.*remote-debugging-port").Run()
	exec.Command("pkill", "-f", "google-chrome.*remote-debugging").Run()

	// Wait a moment for processes to terminate
	time.Sleep(1 * time.Second)
}

func (s *CDPBrowserServer) cleanup() {
	// Close CDP connection
	if s.cancel != nil {
		s.cancel()
	}
	if s.allocCancel != nil {
		s.allocCancel()
	}

	// Always terminate Chrome for testing to avoid conflicts
	if s.chromeCmd != nil && s.chromeCmd.Process != nil {
		log.Println("Terminating Chrome process to avoid conflicts...")
		s.chromeCmd.Process.Kill()
		s.chromeCmd.Wait()
	}
}

func (s *CDPBrowserServer) Initialize() error {
	// Kill any existing Chrome processes first
	s.killExistingChromeProcesses()

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

type TypeTextArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector, DOM ID, or ARIA label for the text input element"`
	Text     string `json:"text" jsonschema:"Text to type into the element"`
	Clear    bool   `json:"clear,omitempty" jsonschema:"Whether to clear existing text before typing (default: false)"`
}

type ClickButtonArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector, DOM ID, or ARIA label for the button element"`
}

type ClickLinkArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector, DOM ID, or ARIA label for the link element"`
}

type SelectDropdownArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector, DOM ID, or ARIA label for the select element"`
	Value    string `json:"value" jsonschema:"Value or visible text of the option to select"`
}

type ChooseOptionArgs struct {
	Selector string `json:"selector" jsonschema:"CSS selector, DOM ID, or ARIA label for the radio button or checkbox"`
	Checked  bool   `json:"checked,omitempty" jsonschema:"Whether to check or uncheck the option (default: true)"`
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

type ARIASnapshotArgs struct {
	Format string `json:"format" jsonschema:"Output format: llm-text, json, debug"`
	Focus  string `json:"focus" jsonschema:"Focus area: all, interactive, landmarks, headings"`
}

// ARIASnapshot tool - captures page accessibility structure for LLM consumption
func (s *CDPBrowserServer) ARIASnapshot(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ARIASnapshotArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	format := req.Params.Arguments.Format
	focus := req.Params.Arguments.Focus

	// Default values
	if format == "" {
		format = "llm-text"
	}
	if focus == "" {
		focus = "all"
	}

	// JavaScript to extract ARIA and DOM structure
	js := `
(function() {
function extractARIASnapshot(focus) {
	const result = {
		page: {
			title: document.title,
			url: window.location.href,
			timestamp: new Date().toISOString()
		},
		landmarks: [],
		interactive: [],
		headings: [],
		content: []
	};
	
	// Helper function to get accessible name
	function getAccessibleName(element) {
		return element.getAttribute('aria-label') || 
			   element.getAttribute('aria-labelledby') && document.getElementById(element.getAttribute('aria-labelledby'))?.textContent ||
			   element.textContent?.trim().substring(0, 100) ||
			   element.getAttribute('title') ||
			   element.getAttribute('placeholder') ||
			   element.getAttribute('alt') ||
			   '';
	}
	
	// Helper function to generate CSS selector with aria-label priority
	function getSelector(element) {
		// Priority 1: aria-label (most specific and semantic)
		const ariaLabel = element.getAttribute('aria-label');
		if (ariaLabel) {
			return '[aria-label="' + ariaLabel.replace(/"/g, '\\"') + '"]';
		}
		
		// Priority 2: ID selector
		if (element.id) return '#' + element.id;
		
		// Priority 3: href for links (semantic)
		if (element.tagName === 'A' && element.getAttribute('href')) {
			return 'a[href="' + element.getAttribute('href') + '"]';
		}
		
		// Priority 4: name attribute for inputs
		if (element.getAttribute('name')) {
			return element.tagName.toLowerCase() + '[name="' + element.getAttribute('name') + '"]';
		}
		
		// Priority 5: type for inputs/buttons
		if (element.getAttribute('type')) {
			return element.tagName.toLowerCase() + '[type="' + element.getAttribute('type') + '"]';
		}
		
		// Priority 6: CSS class (fallback, least reliable)
		let selector = element.tagName.toLowerCase();
		if (element.className) {
			const classes = element.className.split(' ').filter(c => c.length > 0);
			if (classes.length > 0) {
				selector += '.' + classes[0];
			}
		}
		
		return selector;
	}
	
	// Helper function to get all possible selectors for an element
	function getAllSelectors(element) {
		const selectors = [];
		
		// Add aria-label selector if available
		const ariaLabel = element.getAttribute('aria-label');
		if (ariaLabel) {
			selectors.push('[aria-label="' + ariaLabel.replace(/"/g, '\\"') + '"]');
		}
		
		// Add ID selector
		if (element.id) {
			selectors.push('#' + element.id);
		}
		
		// Add href selector for links
		if (element.tagName === 'A' && element.getAttribute('href')) {
			selectors.push('a[href="' + element.getAttribute('href') + '"]');
		}
		
		// Add name/type selectors
		if (element.getAttribute('name')) {
			selectors.push(element.tagName.toLowerCase() + '[name="' + element.getAttribute('name') + '"]');
		}
		if (element.getAttribute('type')) {
			selectors.push(element.tagName.toLowerCase() + '[type="' + element.getAttribute('type') + '"]');
		}
		
		// Add CSS class selector as fallback
		let classSelector = element.tagName.toLowerCase();
		if (element.className) {
			const classes = element.className.split(' ').filter(c => c.length > 0);
			if (classes.length > 0) {
				classSelector += '.' + classes[0];
			}
		}
		selectors.push(classSelector);
		
		return selectors;
	}
	
	// Extract landmarks
	function extractLandmarks() {
		const landmarkRoles = ['banner', 'navigation', 'main', 'contentinfo', 'complementary', 'region', 'search', 'form'];
		const landmarkTags = ['header', 'nav', 'main', 'footer', 'aside', 'section'];
		
		// Find by role
		landmarkRoles.forEach(role => {
			document.querySelectorAll('[role="' + role + '"]').forEach(el => {
				if (el.offsetParent !== null || role === 'banner' || role === 'contentinfo') { // visible or important
					result.landmarks.push({
						role: role,
						name: getAccessibleName(el),
						selector: getSelector(el),
						tag: el.tagName.toLowerCase()
					});
				}
			});
		});
		
		// Find by semantic tags
		landmarkTags.forEach(tag => {
			document.querySelectorAll(tag).forEach(el => {
				if (el.offsetParent !== null && !el.hasAttribute('role')) {
					const implicitRole = tag === 'header' ? 'banner' : 
									   tag === 'nav' ? 'navigation' :
									   tag === 'main' ? 'main' :
									   tag === 'footer' ? 'contentinfo' :
									   tag === 'aside' ? 'complementary' : 'region';
					
					result.landmarks.push({
						role: implicitRole,
						name: getAccessibleName(el),
						selector: getSelector(el),
						tag: tag
					});
				}
			});
		});
	}
	
	// Extract interactive elements
	function extractInteractive() {
		const interactiveSelectors = [
			'button', 'input', 'select', 'textarea', 'a[href]', 
			'[role="button"]', '[role="link"]', '[role="menuitem"]', 
			'[role="tab"]', '[role="checkbox"]', '[role="radio"]',
			'[tabindex]:not([tabindex="-1"])', '[onclick]'
		];
		
		interactiveSelectors.forEach(selector => {
			document.querySelectorAll(selector).forEach(el => {
				if (el.offsetParent !== null && !el.disabled) { // visible and enabled
					const role = el.getAttribute('role') || 
								(el.tagName === 'A' ? 'link' :
								 el.tagName === 'BUTTON' ? 'button' :
								 el.tagName === 'INPUT' ? el.type :
								 el.tagName.toLowerCase());
					
					const ariaLabel = el.getAttribute('aria-label');
					const allSelectors = getAllSelectors(el);
					
					result.interactive.push({
						role: role,
						name: getAccessibleName(el),
						selector: getSelector(el),
						selectors: allSelectors,
						ariaLabel: ariaLabel || '',
						tag: el.tagName.toLowerCase(),
						href: el.href || '',
						value: el.value || ''
					});
				}
			});
		});
	}
	
	// Extract headings
	function extractHeadings() {
		document.querySelectorAll('h1, h2, h3, h4, h5, h6, [role="heading"]').forEach(el => {
			if (el.offsetParent !== null && el.textContent.trim()) {
				const level = el.tagName.match(/H(\d)/) ? el.tagName.charAt(1) : 
							 el.getAttribute('aria-level') || '1';
				
				result.headings.push({
					level: parseInt(level),
					text: el.textContent.trim(),
					selector: getSelector(el),
					tag: el.tagName.toLowerCase()
				});
			}
		});
		
		// Sort by document order
		result.headings.sort((a, b) => {
			const aEl = document.querySelector(a.selector);
			const bEl = document.querySelector(b.selector);
			return aEl && bEl ? (aEl.compareDocumentPosition(bEl) & Node.DOCUMENT_POSITION_FOLLOWING) ? -1 : 1 : 0;
		});
	}
	
	// Extract content structure (simplified)
	function extractContent() {
		document.querySelectorAll('article, section, [role="article"], [role="region"]').forEach(el => {
			if (el.offsetParent !== null) {
				result.content.push({
					role: el.getAttribute('role') || (el.tagName === 'ARTICLE' ? 'article' : 'region'),
					name: getAccessibleName(el),
					selector: getSelector(el),
					tag: el.tagName.toLowerCase()
				});
			}
		});
	}
	
	// Execute based on focus
	if (focus === 'all' || focus === 'landmarks') extractLandmarks();
	if (focus === 'all' || focus === 'interactive') extractInteractive();
	if (focus === 'all' || focus === 'headings') extractHeadings();
	if (focus === 'all') extractContent();
	
	return result;
}

return extractARIASnapshot('` + focus + `');
})();
`

	var ariaData map[string]interface{}
	err := chromedp.Run(s.ctx, chromedp.Evaluate(js, &ariaData))
	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error extracting ARIA snapshot: %v", err)},
			},
			IsError: true,
		}, nil
	}

	// Format output based on request
	var output string
	switch format {
	case "json":
		if jsonBytes, err := json.MarshalIndent(ariaData, "", "  "); err == nil {
			output = string(jsonBytes)
		} else {
			output = fmt.Sprintf("Error formatting JSON: %v", err)
		}
	case "debug":
		output = fmt.Sprintf("ARIA Snapshot Debug:\n%+v", ariaData)
	default: // llm-text
		output = s.formatForLLM(ariaData)
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: output},
		},
	}, nil
}

// formatForLLM converts the ARIA data into LLM-friendly text format
func (s *CDPBrowserServer) formatForLLM(data map[string]interface{}) string {
	var output strings.Builder

	// Page information
	if page, ok := data["page"].(map[string]interface{}); ok {
		output.WriteString(fmt.Sprintf("PAGE: %s (%s)\n\n",
			page["title"], page["url"]))
	}

	// Landmarks
	if landmarks, ok := data["landmarks"].([]interface{}); ok && len(landmarks) > 0 {
		output.WriteString("LANDMARKS:\n")
		for _, item := range landmarks {
			if landmark, ok := item.(map[string]interface{}); ok {
				name := landmark["name"].(string)
				role := landmark["role"].(string)
				if name == "" {
					name = fmt.Sprintf("<%s>", landmark["tag"].(string))
				}
				output.WriteString(fmt.Sprintf("• [%s] %s\n", role, name))
			}
		}
		output.WriteString("\n")
	}

	// Interactive elements
	if interactive, ok := data["interactive"].([]interface{}); ok && len(interactive) > 0 {
		output.WriteString("INTERACTIVE ELEMENTS:\n")
		for _, item := range interactive {
			if elem, ok := item.(map[string]interface{}); ok {
				name := elem["name"].(string)
				role := elem["role"].(string)
				selector := elem["selector"].(string)
				ariaLabel := ""
				if al, exists := elem["ariaLabel"].(string); exists && al != "" {
					ariaLabel = al
				}

				if name == "" {
					name = fmt.Sprintf("<%s>", elem["tag"].(string))
				}

				// Add href or value info if relevant
				extra := ""
				if href, exists := elem["href"].(string); exists && href != "" {
					extra = fmt.Sprintf(" -> %s", href)
				} else if value, exists := elem["value"].(string); exists && value != "" {
					extra = fmt.Sprintf(" value=\"%s\"", value)
				}

				// Format with aria-label if available
				if ariaLabel != "" {
					output.WriteString(fmt.Sprintf("• [%s] \"%s\" (aria-label: \"%s\")%s\n",
						role, name, ariaLabel, extra))
					output.WriteString(fmt.Sprintf("  - Primary selector: %s\n", selector))

					// Show alternative selectors if available
					if selectors, exists := elem["selectors"].([]interface{}); exists && len(selectors) > 1 {
						output.WriteString("  - Alternative selectors: ")
						var altSelectors []string
						for i, sel := range selectors {
							if i > 0 { // Skip the first one (primary)
								if selStr, ok := sel.(string); ok {
									altSelectors = append(altSelectors, selStr)
								}
							}
						}
						output.WriteString(strings.Join(altSelectors, ", ") + "\n")
					}
				} else {
					output.WriteString(fmt.Sprintf("• [%s] \"%s\"%s (selector: %s)\n",
						role, name, extra, selector))
				}
			}
		}
		output.WriteString("\n")
	}

	// Headings
	if headings, ok := data["headings"].([]interface{}); ok && len(headings) > 0 {
		output.WriteString("HEADINGS:\n")
		for _, item := range headings {
			if heading, ok := item.(map[string]interface{}); ok {
				level := int(heading["level"].(float64))
				text := heading["text"].(string)
				indent := strings.Repeat("  ", level-1)
				output.WriteString(fmt.Sprintf("%s• [h%d] \"%s\"\n", indent, level, text))
			}
		}
		output.WriteString("\n")
	}

	// Content structure
	if content, ok := data["content"].([]interface{}); ok && len(content) > 0 {
		output.WriteString("CONTENT STRUCTURE:\n")
		for _, item := range content {
			if section, ok := item.(map[string]interface{}); ok {
				name := section["name"].(string)
				role := section["role"].(string)
				if name == "" {
					name = fmt.Sprintf("<%s>", section["tag"].(string))
				}
				output.WriteString(fmt.Sprintf("• [%s] %s\n", role, name))
			}
		}
		output.WriteString("\n")
	}

	return output.String()
}

// findElementWithSmartSelector attempts to find an element using multiple targeting strategies with native CDP
func (s *CDPBrowserServer) findElementWithSmartSelector(selector string) (string, error) {
	log.Printf("Smart selector: Trying to find element with selector '%s'", selector)

	// Strategy 1: Try aria-label first (most semantic and reliable)
	ariaSelector := fmt.Sprintf(`[aria-label="%s"]`, selector)
	var nodes []*cdp.Node
	err := chromedp.Run(s.ctx, chromedp.Nodes(ariaSelector, &nodes, chromedp.ByQuery))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using aria-label: %s", ariaSelector)
		return ariaSelector, nil
	}
	log.Printf("Smart selector: aria-label strategy failed for '%s'", ariaSelector)

	// Strategy 2: Try the selector as-is (direct CSS selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using direct selector: %s", selector)
		return selector, nil
	}
	log.Printf("Smart selector: Direct selector failed for '%s'", selector)

	// Strategy 3: If it looks like an ID, try with # prefix
	if !strings.HasPrefix(selector, "#") && !strings.Contains(selector, ".") && !strings.Contains(selector, "[") && !strings.Contains(selector, " ") {
		idSelector := "#" + selector
		err = chromedp.Run(s.ctx, chromedp.Nodes(idSelector, &nodes, chromedp.ByQuery))
		if err == nil && len(nodes) > 0 {
			log.Printf("Smart selector: Found element using ID selector: %s", idSelector)
			return idSelector, nil
		}
		log.Printf("Smart selector: ID selector failed for '%s'", idSelector)
	}

	// Strategy 4: Try partial aria-label match (contains)
	partialAriaSelector := fmt.Sprintf(`[aria-label*="%s"]`, selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(partialAriaSelector, &nodes, chromedp.ByQuery))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using partial aria-label: %s", partialAriaSelector)
		return partialAriaSelector, nil
	}
	log.Printf("Smart selector: Partial aria-label strategy failed for '%s'", partialAriaSelector)

	// Strategy 5: Try name attribute for form elements
	nameSelector := fmt.Sprintf(`[name="%s"]`, selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(nameSelector, &nodes, chromedp.ByQuery))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using name attribute: %s", nameSelector)
		return nameSelector, nil
	}
	log.Printf("Smart selector: Name attribute strategy failed for '%s'", nameSelector)

	// Strategy 6: Try placeholder attribute for inputs
	placeholderSelector := fmt.Sprintf(`[placeholder="%s"]`, selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(placeholderSelector, &nodes, chromedp.ByQuery))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using placeholder: %s", placeholderSelector)
		return placeholderSelector, nil
	}
	log.Printf("Smart selector: Placeholder strategy failed for '%s'", placeholderSelector)

	// Strategy 7: Try text content matching for buttons and links using XPath
	textXPath := fmt.Sprintf(`//button[text()="%s"] | //a[text()="%s"] | //input[@value="%s"]`, selector, selector, selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(textXPath, &nodes, chromedp.BySearch))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using exact text XPath: %s", textXPath)
		return textXPath, nil
	}
	log.Printf("Smart selector: Exact text XPath failed for '%s'", textXPath)

	// Strategy 8: Try partial text content matching
	partialTextXPath := fmt.Sprintf(`//button[contains(text(), "%s")] | //a[contains(text(), "%s")] | //input[contains(@value, "%s")]`, selector, selector, selector)
	err = chromedp.Run(s.ctx, chromedp.Nodes(partialTextXPath, &nodes, chromedp.BySearch))
	if err == nil && len(nodes) > 0 {
		log.Printf("Smart selector: Found element using partial text XPath: %s", partialTextXPath)
		return partialTextXPath, nil
	}
	log.Printf("Smart selector: Partial text XPath failed for '%s'", partialTextXPath)

	// If no strategy worked, return original selector and let ChromeDP handle the error
	log.Printf("Smart selector: All strategies failed for '%s'", selector)
	return selector, fmt.Errorf("element not found with any targeting strategy: %s", selector)
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

// TypeText tool - types text into an input element
func (s *CDPBrowserServer) TypeText(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[TypeTextArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector
	text := req.Params.Arguments.Text
	clear := req.Params.Arguments.Clear

	log.Printf("TypeText called: selector='%s', text='%s', clear=%t", selector, text, clear)

	// Create a timeout context for the entire operation
	timeoutCtx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()

	log.Printf("TypeText: Step 1 - Testing if element exists...")
	// First, check if element exists at all
	var nodes []*cdp.Node
	err := chromedp.Run(timeoutCtx, chromedp.Nodes(selector, &nodes, chromedp.ByQuery))
	if err != nil {
		log.Printf("TypeText: Step 1 FAILED - Element query error: %v", err)
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Element query failed for %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}

	if len(nodes) == 0 {
		log.Printf("TypeText: Step 1 FAILED - No elements found with selector: %s", selector)
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("No elements found with selector: %s", selector)},
			},
			IsError: true,
		}, nil
	}

	log.Printf("TypeText: Step 1 SUCCESS - Found %d elements", len(nodes))

	log.Printf("TypeText: Step 2 - Waiting for element to be visible...")
	// Wait for element to be visible with shorter timeout
	err = chromedp.Run(timeoutCtx, chromedp.WaitVisible(selector, chromedp.ByQuery))
	if err != nil {
		log.Printf("TypeText: Step 2 FAILED - WaitVisible error: %v", err)
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Element not visible %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}
	log.Printf("TypeText: Step 2 SUCCESS - Element is visible")

	if clear {
		log.Printf("TypeText: Step 3 - Clearing element...")
		err = chromedp.Run(timeoutCtx, chromedp.Clear(selector, chromedp.ByQuery))
		if err != nil {
			log.Printf("TypeText: Step 3 FAILED - Clear error: %v", err)
			return &mcp.CallToolResultFor[struct{}]{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Clear failed for %s: %v", selector, err)},
				},
				IsError: true,
			}, nil
		}
		log.Printf("TypeText: Step 3 SUCCESS - Element cleared")
	}

	log.Printf("TypeText: Step 4 - Sending keys...")
	err = chromedp.Run(timeoutCtx, chromedp.SendKeys(selector, text, chromedp.ByQuery))
	if err != nil {
		log.Printf("TypeText: Step 4 FAILED - SendKeys error: %v", err)
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("SendKeys failed for %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}

	log.Printf("TypeText: All steps successful! Typed '%s' into '%s'", text, selector)
	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Typed \"%s\" into element: %s", text, selector)},
		},
	}, nil
}

// ClickButton tool - clicks a button element
func (s *CDPBrowserServer) ClickButton(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ClickButtonArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector

	log.Printf("ClickButton called: selector='%s'", selector)

	// Use smart selector to find the best targeting strategy
	smartSelector, smartErr := s.findElementWithSmartSelector(selector)
	if smartErr == nil {
		log.Printf("ClickButton: Using smart selector: '%s'", smartSelector)

		// Determine the right chromedp strategy based on selector type
		if strings.HasPrefix(smartSelector, "//") {
			// XPath selector
			err := chromedp.Run(s.ctx, chromedp.WaitVisible(smartSelector, chromedp.BySearch), chromedp.Click(smartSelector, chromedp.BySearch))
			if err == nil {
				log.Printf("ClickButton: Successfully clicked button using XPath: '%s'", smartSelector)
				return &mcp.CallToolResultFor[struct{}]{
					Content: []mcp.Content{
						&mcp.TextContent{Text: fmt.Sprintf("Clicked button: %s", smartSelector)},
					},
				}, nil
			}
			log.Printf("ClickButton: XPath smart selector failed: %v", err)
		} else {
			// CSS selector
			err := chromedp.Run(s.ctx, chromedp.WaitVisible(smartSelector, chromedp.ByQuery), chromedp.Click(smartSelector, chromedp.ByQuery))
			if err == nil {
				log.Printf("ClickButton: Successfully clicked button using CSS: '%s'", smartSelector)
				return &mcp.CallToolResultFor[struct{}]{
					Content: []mcp.Content{
						&mcp.TextContent{Text: fmt.Sprintf("Clicked button: %s", smartSelector)},
					},
				}, nil
			}
			log.Printf("ClickButton: CSS smart selector failed: %v", err)
		}
	} else {
		log.Printf("ClickButton: Smart selector failed: %v", smartErr)
	}

	// Fallback to original logic
	log.Printf("ClickButton: Trying fallback with original selector: '%s'", selector)
	err := chromedp.Run(s.ctx, chromedp.WaitVisible(selector, chromedp.ByQuery), chromedp.Click(selector, chromedp.ByQuery))
	if err != nil {
		log.Printf("ClickButton: Primary selector failed: %v", err)
		// Try with exact text matching using XPath
		textXPath := fmt.Sprintf(`//button[text()="%s"] | //input[@value="%s"]`, selector, selector)
		log.Printf("ClickButton: Trying XPath fallback: '%s'", textXPath)
		err = chromedp.Run(s.ctx, chromedp.WaitVisible(textXPath, chromedp.BySearch), chromedp.Click(textXPath, chromedp.BySearch))
		if err == nil {
			log.Printf("ClickButton: XPath fallback succeeded")
			selector = textXPath // Update for response message
		} else {
			log.Printf("ClickButton: XPath fallback also failed: %v", err)
		}
	} else {
		log.Printf("ClickButton: Primary selector succeeded")
	}

	if err != nil {
		log.Printf("ClickButton: All attempts failed, returning error")
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error clicking button %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}

	log.Printf("ClickButton: Successfully clicked button '%s'", selector)
	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Clicked button: %s", selector)},
		},
	}, nil
}

// ClickLink tool - clicks a link element
func (s *CDPBrowserServer) ClickLink(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ClickLinkArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector

	log.Printf("ClickLink called: selector='%s'", selector)

	// Use smart selector to find the best targeting strategy
	smartSelector, smartErr := s.findElementWithSmartSelector(selector)
	if smartErr == nil {
		log.Printf("ClickLink: Using smart selector: '%s'", smartSelector)

		// Determine the right chromedp strategy based on selector type
		if strings.HasPrefix(smartSelector, "//") {
			// XPath selector
			err := chromedp.Run(s.ctx, chromedp.WaitVisible(smartSelector, chromedp.BySearch), chromedp.Click(smartSelector, chromedp.BySearch))
			if err == nil {
				log.Printf("ClickLink: Successfully clicked link using XPath: '%s'", smartSelector)
				return &mcp.CallToolResultFor[struct{}]{
					Content: []mcp.Content{
						&mcp.TextContent{Text: fmt.Sprintf("Clicked link: %s", smartSelector)},
					},
				}, nil
			}
			log.Printf("ClickLink: XPath smart selector failed: %v", err)
		} else {
			// CSS selector
			err := chromedp.Run(s.ctx, chromedp.WaitVisible(smartSelector, chromedp.ByQuery), chromedp.Click(smartSelector, chromedp.ByQuery))
			if err == nil {
				log.Printf("ClickLink: Successfully clicked link using CSS: '%s'", smartSelector)
				return &mcp.CallToolResultFor[struct{}]{
					Content: []mcp.Content{
						&mcp.TextContent{Text: fmt.Sprintf("Clicked link: %s", smartSelector)},
					},
				}, nil
			}
			log.Printf("ClickLink: CSS smart selector failed: %v", err)
		}
	} else {
		log.Printf("ClickLink: Smart selector failed: %v", smartErr)
	}

	// Fallback to original logic
	log.Printf("ClickLink: Trying fallback with original selector: '%s'", selector)
	err := chromedp.Run(s.ctx, chromedp.WaitVisible(selector, chromedp.ByQuery), chromedp.Click(selector, chromedp.ByQuery))
	if err != nil {
		// Try with text content matching using XPath
		textXPath := fmt.Sprintf(`//a[text()="%s"]`, selector)
		log.Printf("ClickLink: Trying XPath fallback: '%s'", textXPath)
		err = chromedp.Run(s.ctx, chromedp.WaitVisible(textXPath, chromedp.BySearch), chromedp.Click(textXPath, chromedp.BySearch))
		if err == nil {
			log.Printf("ClickLink: XPath fallback succeeded")
			selector = textXPath
		} else {
			log.Printf("ClickLink: XPath fallback also failed: %v", err)
		}
	} else {
		log.Printf("ClickLink: Original selector succeeded")
	}

	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error clicking link %s: %v", selector, err)},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Clicked link: %s", selector)},
		},
	}, nil
}

// SelectDropdown tool - selects an option from a dropdown
func (s *CDPBrowserServer) SelectDropdown(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[SelectDropdownArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector
	value := req.Params.Arguments.Value

	// Try direct selection first
	err := chromedp.Run(s.ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SetAttributeValue(selector, "value", value, chromedp.ByQuery),
	)

	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error selecting option \"%s\" from dropdown %s: %v", value, selector, err)},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Selected option \"%s\" from dropdown: %s", value, selector)},
		},
	}, nil
}

// ChooseOption tool - checks/unchecks a radio button or checkbox
func (s *CDPBrowserServer) ChooseOption(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[ChooseOptionArgs]]) (*mcp.CallToolResultFor[struct{}], error) {
	selector := req.Params.Arguments.Selector
	checked := req.Params.Arguments.Checked
	if !req.Params.Arguments.Checked && req.Params.Arguments.Checked == false {
		checked = true // default to true if not specified
	}

	// Use ChromeDP's native SetAttributeValue for checkboxes/radio buttons
	err := chromedp.Run(s.ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SetAttributeValue(selector, "checked", fmt.Sprintf("%t", checked), chromedp.ByQuery),
	)

	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error setting option %s to %t: %v", selector, checked, err)},
			},
			IsError: true,
		}, nil
	}

	action := "checked"
	if !checked {
		action = "unchecked"
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: fmt.Sprintf("Option %s: %s", action, selector)},
		},
	}, nil
}

// RefreshPage tool - refreshes the current page
func (s *CDPBrowserServer) RefreshPage(ctx context.Context, req *mcp.ServerRequest[*mcp.CallToolParamsFor[struct{}]]) (*mcp.CallToolResultFor[struct{}], error) {
	err := chromedp.Run(s.ctx, chromedp.Reload())
	if err != nil {
		return &mcp.CallToolResultFor[struct{}]{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Error refreshing page: %v", err)},
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResultFor[struct{}]{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "Page refreshed successfully"},
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

	log.Println("Registering MCP tools...")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "navigate", Description: "Navigate to a URL"}, server.Navigate)
	log.Println("Registered tool: navigate")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "click", Description: "Click on an element"}, server.Click)
	log.Println("Registered tool: click")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "screenshot", Description: "Take a screenshot"}, server.Screenshot)
	log.Println("Registered tool: screenshot")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "aria_snapshot", Description: "Capture ARIA accessibility structure for LLM analysis"}, server.ARIASnapshot)
	log.Println("Registered tool: aria_snapshot")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "type_text", Description: "Type text into an input field with smart element targeting"}, server.TypeText)
	log.Println("Registered tool: type_text")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "click_button", Description: "Click a button element with smart targeting"}, server.ClickButton)
	log.Println("Registered tool: click_button")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "click_link", Description: "Click a link element with smart targeting"}, server.ClickLink)
	log.Println("Registered tool: click_link")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "select_dropdown", Description: "Select an option from a dropdown with smart targeting"}, server.SelectDropdown)
	log.Println("Registered tool: select_dropdown")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "choose_option", Description: "Check/uncheck a radio button or checkbox with smart targeting"}, server.ChooseOption)
	log.Println("Registered tool: choose_option")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "refresh_page", Description: "Refresh the current page"}, server.RefreshPage)
	log.Println("Registered tool: refresh_page")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "close_browser", Description: "Close the Chrome browser"}, server.CloseBrowser)
	log.Println("Registered tool: close_browser")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "set_chrome_lifecycle", Description: "Control whether Chrome stays open when MCP server exits"}, server.SetChromeLifecycle)
	log.Println("Registered tool: set_chrome_lifecycle")
	mcp.AddTool(mcpServer, &mcp.Tool{Name: "shutdown_server", Description: "Gracefully shutdown the MCP server"}, server.ShutdownServer)
	log.Println("Registered tool: shutdown_server")
	log.Println("All tools registered successfully")

	transport := &mcp.StdioTransport{}

	log.Println("Server ready - waiting for MCP requests on STDIO")
	if err := mcpServer.Run(context.Background(), transport); err != nil {
		log.Printf("Server stopped with error: %v", err)
	}

	log.Println("Server shutdown complete")
}
