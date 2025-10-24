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
