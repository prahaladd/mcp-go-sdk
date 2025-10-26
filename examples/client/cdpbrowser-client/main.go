// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// The cdpbrowser-client command demonstrates how to interact with the cdpbrowser MCP server.
// This client spawns the cdpbrowser server as a subprocess and communicates via STDIO.
//
// Usage: cdpbrowser-client <command> [<args>]
//
// Available commands:
//
//	navigate <url>     - Navigate to a URL
//	click <selector>   - Click on an element using CSS selector
//	screenshot         - Take a screenshot of the current page
//	close              - Close the browser
//	lifecycle <bool>   - Set Chrome lifecycle (true=keep open, false=close on exit)
//	demo              - Run a demo sequence
//	list-tools        - List available tools from the server
//
// Example:
//
//	cdpbrowser-client navigate https://example.com
//	cdpbrowser-client click "button.submit"
//	cdpbrowser-client screenshot
//	cdpbrowser-client demo
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Create the MCP client
	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "cdpbrowser-client",
		Version: "v1.0.0",
	}, nil)

	// Get the path to the server executable
	// Assuming we're running from the client directory, the server is at ../../server/cdpbrowser/
	serverPath := filepath.Join("..", "..", "server", "cdpbrowser", "cdpbrowser")

	// Create command to run the server
	serverCmd := exec.Command(serverPath)

	fmt.Printf("Starting cdpbrowser server: %s\n", serverPath)

	// Connect to the server via STDIO transport
	cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: serverCmd}, nil)
	if err != nil {
		log.Fatalf("Failed to connect to cdpbrowser server: %v", err)
	}
	defer func() {
		fmt.Println("Closing connection to server...")
		cs.Close()
	}()

	switch command {
	case "navigate":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s navigate <url>\n", os.Args[0])
			os.Exit(1)
		}
		navigate(ctx, cs, os.Args[2])

	case "click":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s click <css-selector>\n", os.Args[0])
			os.Exit(1)
		}
		click(ctx, cs, os.Args[2])

	case "screenshot":
		screenshot(ctx, cs)

	case "aria-snapshot":
		format := "llm-text"
		focus := "all"
		if len(os.Args) > 2 {
			format = os.Args[2]
		}
		if len(os.Args) > 3 {
			focus = os.Args[3]
		}
		ariaSnapshot(ctx, cs, format, focus)

	case "type-text":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s type-text <selector> <text> [clear]\n", os.Args[0])
			os.Exit(1)
		}
		clear := false
		if len(os.Args) > 4 {
			clear = os.Args[4] == "true"
		}
		typeText(ctx, cs, os.Args[2], os.Args[3], clear)

	case "click-button":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s click-button <selector>\n", os.Args[0])
			os.Exit(1)
		}
		clickButton(ctx, cs, os.Args[2])

	case "click-link":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s click-link <selector>\n", os.Args[0])
			os.Exit(1)
		}
		clickLink(ctx, cs, os.Args[2])

	case "select-dropdown":
		if len(os.Args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: %s select-dropdown <selector> <value>\n", os.Args[0])
			os.Exit(1)
		}
		selectDropdown(ctx, cs, os.Args[2], os.Args[3])

	case "choose-option":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s choose-option <selector> [checked]\n", os.Args[0])
			os.Exit(1)
		}
		checked := true
		if len(os.Args) > 3 {
			checked = os.Args[3] == "true"
		}
		chooseOption(ctx, cs, os.Args[2], checked)

	case "refresh":
		refreshPage(ctx, cs)

	case "close":
		closeBrowser(ctx, cs)

	case "lifecycle":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s lifecycle <true|false>\n", os.Args[0])
			os.Exit(1)
		}
		keepOpen, err := strconv.ParseBool(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid boolean value: %s\n", os.Args[2])
			os.Exit(1)
		}
		setLifecycle(ctx, cs, keepOpen)

	case "demo":
		runDemo(ctx, cs)

	case "list-tools":
		listTools(ctx, cs)

	case "interactive":
		runInteractive(ctx, cs)

	case "run-script":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: %s run-script <script-file>\n", os.Args[0])
			os.Exit(1)
		}
		runScript(ctx, cs, os.Args[2])

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf("Usage: %s <command> [<args>]\n\n", os.Args[0])
	fmt.Println("Available commands:")
	fmt.Println("  navigate <url>     - Navigate to a URL")
	fmt.Println("  click <selector>   - Click on an element using CSS selector")
	fmt.Println("  screenshot         - Take a screenshot of the current page")
	fmt.Println("  aria-snapshot [format] [focus] - Capture ARIA accessibility structure")
	fmt.Println("  type-text <selector> <text> [clear] - Type text into an input field")
	fmt.Println("  click-button <selector> - Click a button element")
	fmt.Println("  click-link <selector> - Click a link element")
	fmt.Println("  select-dropdown <selector> <value> - Select an option from a dropdown")
	fmt.Println("  choose-option <selector> [checked] - Check/uncheck a radio button or checkbox")
	fmt.Println("  refresh            - Refresh the current page")
	fmt.Println("  close              - Close the browser")
	fmt.Println("  lifecycle <bool>   - Set Chrome lifecycle (true=keep open, false=close on exit)")
	fmt.Println("  list-tools         - List all available tools from the server")
	fmt.Println("  interactive        - Start interactive mode for multiple commands")
	fmt.Println("  run-script <file>  - Execute commands from a script file")
	fmt.Println("  demo              - Run a demo sequence")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s navigate https://example.com\n", os.Args[0])
	fmt.Printf("  %s click \"button.submit\"\n", os.Args[0])
	fmt.Printf("  %s screenshot\n", os.Args[0])
	fmt.Printf("  %s aria-snapshot\n", os.Args[0])
	fmt.Printf("  %s type-text \"#email\" \"user@example.com\"\n", os.Args[0])
	fmt.Printf("  %s click-button \"Submit\"\n", os.Args[0])
	fmt.Printf("  %s select-dropdown \"country\" \"United States\"\n", os.Args[0])
	fmt.Printf("  %s choose-option \"newsletter\" true\n", os.Args[0])
	fmt.Printf("  %s interactive\n", os.Args[0])
	fmt.Printf("  %s run-script actions.txt\n", os.Args[0])
	fmt.Printf("  %s demo\n", os.Args[0])
}

func navigate(ctx context.Context, cs *mcp.ClientSession, url string) {
	fmt.Printf("Navigating to: %s\n", url)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "navigate",
		Arguments: map[string]interface{}{
			"url": url,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call navigate tool: %v", err)
	}

	printToolResult(result)
}

func click(ctx context.Context, cs *mcp.ClientSession, selector string) {
	fmt.Printf("Clicking element with selector: %s\n", selector)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "click",
		Arguments: map[string]interface{}{
			"selector": selector,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call click tool: %v", err)
	}

	printToolResult(result)
}

func screenshot(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Taking screenshot...")

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "screenshot",
		Arguments: map[string]interface{}{},
	})

	if err != nil {
		log.Fatalf("Failed to call screenshot tool: %v", err)
	}

	printToolResult(result)
}

func ariaSnapshot(ctx context.Context, cs *mcp.ClientSession, format, focus string) {
	fmt.Printf("Taking ARIA snapshot (format: %s, focus: %s)...\n", format, focus)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "aria_snapshot",
		Arguments: map[string]interface{}{
			"format": format,
			"focus":  focus,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call aria_snapshot tool: %v", err)
	}

	printToolResult(result)
}

func typeText(ctx context.Context, cs *mcp.ClientSession, selector, text string, clear bool) {
	fmt.Printf("Typing text \"%s\" into element: %s (clear: %t)\n", text, selector, clear)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "type_text",
		Arguments: map[string]interface{}{
			"selector": selector,
			"text":     text,
			"clear":    clear,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call type_text tool: %v", err)
	}

	printToolResult(result)
}

func clickButton(ctx context.Context, cs *mcp.ClientSession, selector string) {
	fmt.Printf("Clicking button: %s\n", selector)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "click_button",
		Arguments: map[string]interface{}{
			"selector": selector,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call click_button tool: %v", err)
	}

	printToolResult(result)
}

func clickLink(ctx context.Context, cs *mcp.ClientSession, selector string) {
	fmt.Printf("Clicking link: %s\n", selector)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "click_link",
		Arguments: map[string]interface{}{
			"selector": selector,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call click_link tool: %v", err)
	}

	printToolResult(result)
}

func selectDropdown(ctx context.Context, cs *mcp.ClientSession, selector, value string) {
	fmt.Printf("Selecting \"%s\" from dropdown: %s\n", value, selector)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "select_dropdown",
		Arguments: map[string]interface{}{
			"selector": selector,
			"value":    value,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call select_dropdown tool: %v", err)
	}

	printToolResult(result)
}

func chooseOption(ctx context.Context, cs *mcp.ClientSession, selector string, checked bool) {
	fmt.Printf("Setting option %s to %t\n", selector, checked)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "choose_option",
		Arguments: map[string]interface{}{
			"selector": selector,
			"checked":  checked,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call choose_option tool: %v", err)
	}

	printToolResult(result)
}

func refreshPage(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Refreshing page...")

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "refresh_page",
		Arguments: map[string]interface{}{},
	})

	if err != nil {
		log.Fatalf("Failed to call refresh_page tool: %v", err)
	}

	printToolResult(result)
}

func closeBrowser(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Closing browser...")

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      "close_browser",
		Arguments: map[string]interface{}{},
	})

	if err != nil {
		log.Fatalf("Failed to call close_browser tool: %v", err)
	}

	printToolResult(result)
}

func listTools(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Listing available tools from server...")

	// Get tools from the server
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			log.Fatalf("Failed to list tools: %v", err)
		}
		fmt.Printf("- %s: %s\n", tool.Name, tool.Description)
	}
}

func setLifecycle(ctx context.Context, cs *mcp.ClientSession, keepOpen bool) {
	fmt.Printf("Setting Chrome lifecycle - keep open: %t\n", keepOpen)

	result, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: "set_chrome_lifecycle",
		Arguments: map[string]interface{}{
			"keep_open": keepOpen,
		},
	})

	if err != nil {
		log.Fatalf("Failed to call set_chrome_lifecycle tool: %v", err)
	}

	printToolResult(result)
}

func runDemo(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Running demo sequence...")

	// Set Chrome to stay open
	fmt.Println("\n1. Setting Chrome to stay open...")
	setLifecycle(ctx, cs, true)
	time.Sleep(1 * time.Second)

	// Navigate to example.com
	fmt.Println("\n2. Navigating to example.com...")
	navigate(ctx, cs, "https://example.com")
	time.Sleep(2 * time.Second)

	// Take a screenshot
	fmt.Println("\n3. Taking screenshot...")
	screenshot(ctx, cs)
	time.Sleep(1 * time.Second)

	// Navigate to another site
	fmt.Println("\n4. Navigating to httpbin.org...")
	navigate(ctx, cs, "https://httpbin.org")
	time.Sleep(2 * time.Second)

	// Take another screenshot
	fmt.Println("\n5. Taking another screenshot...")
	screenshot(ctx, cs)

	fmt.Println("\nDemo completed! Chrome browser will remain open.")
}

func runInteractive(ctx context.Context, cs *mcp.ClientSession) {
	fmt.Println("Starting interactive mode. Type 'help' for commands or 'exit' to quit.")
	fmt.Println("Server connection maintained for multiple commands.")

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("cdp> ")
		if !scanner.Scan() {
			break
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		command := parts[0]

		switch command {
		case "exit", "quit":
			fmt.Println("Exiting interactive mode...")
			return

		case "help":
			fmt.Println("Available commands:")
			fmt.Println("  navigate <url>     - Navigate to a URL")
			fmt.Println("  click <selector>   - Click on an element")
			fmt.Println("  screenshot         - Take a screenshot")
			fmt.Println("  aria-snapshot [format] [focus] - Capture ARIA structure")
			fmt.Println("  type-text <selector> <text> [clear] - Type text into an input field")
			fmt.Println("  click-button <selector> - Click a button element")
			fmt.Println("  click-link <selector> - Click a link element")
			fmt.Println("  select-dropdown <selector> <value> - Select dropdown option")
			fmt.Println("  choose-option <selector> [checked] - Check/uncheck option")
			fmt.Println("  refresh            - Refresh the current page")
			fmt.Println("  close              - Close browser")
			fmt.Println("  lifecycle <bool>   - Set Chrome lifecycle")
			fmt.Println("  list-tools         - List available tools")
			fmt.Println("  help               - Show this help")
			fmt.Println("  exit/quit          - Exit interactive mode")

		case "navigate":
			if len(parts) < 2 {
				fmt.Println("Usage: navigate <url>")
				continue
			}
			navigate(ctx, cs, parts[1])

		case "click":
			if len(parts) < 2 {
				fmt.Println("Usage: click <css-selector>")
				continue
			}
			click(ctx, cs, parts[1])

		case "screenshot":
			screenshot(ctx, cs)

		case "aria-snapshot":
			format := "llm-text"
			focus := "all"
			if len(parts) > 1 {
				format = parts[1]
			}
			if len(parts) > 2 {
				focus = parts[2]
			}
			ariaSnapshot(ctx, cs, format, focus)

		case "type-text":
			if len(parts) < 3 {
				fmt.Println("Usage: type-text <selector> <text> [clear]")
				continue
			}
			clear := false
			if len(parts) > 3 {
				clear = parts[3] == "true"
			}
			// Join all parts from index 2 onwards to handle text with spaces
			text := strings.Join(parts[2:len(parts)], " ")
			if len(parts) > 3 && parts[len(parts)-1] == "true" {
				text = strings.Join(parts[2:len(parts)-1], " ")
				clear = true
			}
			typeText(ctx, cs, parts[1], text, clear)

		case "click-button":
			if len(parts) < 2 {
				fmt.Println("Usage: click-button <selector>")
				continue
			}
			clickButton(ctx, cs, parts[1])

		case "click-link":
			if len(parts) < 2 {
				fmt.Println("Usage: click-link <selector>")
				continue
			}
			clickLink(ctx, cs, parts[1])

		case "select-dropdown":
			if len(parts) < 3 {
				fmt.Println("Usage: select-dropdown <selector> <value>")
				continue
			}
			// Join all parts from index 2 onwards to handle values with spaces
			value := strings.Join(parts[2:], " ")
			selectDropdown(ctx, cs, parts[1], value)

		case "choose-option":
			if len(parts) < 2 {
				fmt.Println("Usage: choose-option <selector> [checked]")
				continue
			}
			checked := true
			if len(parts) > 2 {
				checked = parts[2] == "true"
			}
			chooseOption(ctx, cs, parts[1], checked)

		case "refresh":
			refreshPage(ctx, cs)

		case "close":
			closeBrowser(ctx, cs)

		case "lifecycle":
			if len(parts) < 2 {
				fmt.Println("Usage: lifecycle <true|false>")
				continue
			}
			keepOpen, err := strconv.ParseBool(parts[1])
			if err != nil {
				fmt.Printf("Invalid boolean value: %s\n", parts[1])
				continue
			}
			setLifecycle(ctx, cs, keepOpen)

		case "list-tools":
			listTools(ctx, cs)

		case "wait":
			if len(parts) < 2 {
				fmt.Println("Usage: wait <seconds>")
				continue
			}
			seconds, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("Invalid wait duration: %s\n", parts[1])
				continue
			}
			fmt.Printf("Waiting %d seconds...\n", seconds)
			time.Sleep(time.Duration(seconds) * time.Second)

		default:
			fmt.Printf("Unknown command: %s (type 'help' for available commands)\n", command)
		}
	}
}

func runScript(ctx context.Context, cs *mcp.ClientSession, scriptFile string) {
	fmt.Printf("Running script file: %s\n", scriptFile)

	// Read the script file
	file, err := os.Open(scriptFile)
	if err != nil {
		log.Fatalf("Failed to open script file %s: %v", scriptFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	fmt.Println("Executing script commands...")
	fmt.Println("=" + strings.Repeat("=", 50))

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fmt.Printf("Line %d: %s\n", lineNum, line)

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		command := parts[0]

		switch command {
		case "navigate":
			if len(parts) < 2 {
				fmt.Printf("  Error: navigate requires URL (line %d)\n", lineNum)
				continue
			}
			navigate(ctx, cs, parts[1])

		case "click":
			if len(parts) < 2 {
				fmt.Printf("  Error: click requires CSS selector (line %d)\n", lineNum)
				continue
			}
			click(ctx, cs, parts[1])

		case "screenshot":
			screenshot(ctx, cs)

		case "aria-snapshot":
			format := "llm-text"
			focus := "all"
			if len(parts) > 1 {
				format = parts[1]
			}
			if len(parts) > 2 {
				focus = parts[2]
			}
			ariaSnapshot(ctx, cs, format, focus)

		case "type-text":
			if len(parts) < 3 {
				fmt.Printf("  Error: type-text requires selector and text (line %d)\n", lineNum)
				continue
			}
			clear := false
			text := strings.Join(parts[2:], " ")
			// Check if last argument is "true" for clear flag
			if len(parts) > 3 && parts[len(parts)-1] == "true" {
				text = strings.Join(parts[2:len(parts)-1], " ")
				clear = true
			}
			typeText(ctx, cs, parts[1], text, clear)

		case "click-button":
			if len(parts) < 2 {
				fmt.Printf("  Error: click-button requires selector (line %d)\n", lineNum)
				continue
			}
			clickButton(ctx, cs, parts[1])

		case "click-link":
			if len(parts) < 2 {
				fmt.Printf("  Error: click-link requires selector (line %d)\n", lineNum)
				continue
			}
			clickLink(ctx, cs, parts[1])

		case "select-dropdown":
			if len(parts) < 3 {
				fmt.Printf("  Error: select-dropdown requires selector and value (line %d)\n", lineNum)
				continue
			}
			value := strings.Join(parts[2:], " ")
			selectDropdown(ctx, cs, parts[1], value)

		case "choose-option":
			if len(parts) < 2 {
				fmt.Printf("  Error: choose-option requires selector (line %d)\n", lineNum)
				continue
			}
			checked := true
			if len(parts) > 2 {
				checked = parts[2] == "true"
			}
			chooseOption(ctx, cs, parts[1], checked)

		case "refresh":
			refreshPage(ctx, cs)

		case "close":
			closeBrowser(ctx, cs)

		case "lifecycle":
			if len(parts) < 2 {
				fmt.Printf("  Error: lifecycle requires boolean value (line %d)\n", lineNum)
				continue
			}
			keepOpen, err := strconv.ParseBool(parts[1])
			if err != nil {
				fmt.Printf("  Error: invalid boolean value '%s' (line %d)\n", parts[1], lineNum)
				continue
			}
			setLifecycle(ctx, cs, keepOpen)

		case "list-tools":
			listTools(ctx, cs)

		case "wait":
			// Optional wait command for delays between actions
			if len(parts) < 2 {
				fmt.Printf("  Error: wait requires duration in seconds (line %d)\n", lineNum)
				continue
			}
			seconds, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Printf("  Error: invalid wait duration '%s' (line %d)\n", parts[1], lineNum)
				continue
			}
			fmt.Printf("  Waiting %d seconds...\n", seconds)
			time.Sleep(time.Duration(seconds) * time.Second)

		default:
			fmt.Printf("  Error: unknown command '%s' (line %d)\n", command, lineNum)
		}

		fmt.Println() // Add spacing between commands
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading script file: %v", err)
	}

	fmt.Println("=" + strings.Repeat("=", 50))
	fmt.Println("Script execution completed")
}

func printToolResult(result *mcp.CallToolResult) {
	if result.IsError {
		fmt.Printf("Error: ")
	} else {
		fmt.Printf("Success: ")
	}

	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			fmt.Println(c.Text)
		case *mcp.ImageContent:
			// Save screenshot to file with timestamp
			timestamp := time.Now().Format("20060102_150405")
			filename := fmt.Sprintf("screenshot_%s.png", timestamp)

			if err := os.WriteFile(filename, c.Data, 0644); err != nil {
				fmt.Printf("Image: %s (size: %d bytes) - Failed to save: %v\n", c.MIMEType, len(c.Data), err)
			} else {
				fmt.Printf("Screenshot saved to: %s (size: %d bytes)\n", filename, len(c.Data))
			}
		default:
			fmt.Printf("Unknown content type: %T\n", content)
		}
	}
}
