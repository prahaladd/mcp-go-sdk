// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// The voicebrowser command connects to cdpbrowser server and uses OpenAI API for browser automation.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openai "github.com/sashabaranov/go-openai"
)

// Global MCP session for tool execution
var globalMCPSession *mcp.ClientSession

// Global flag to track if initial login prompt has been shown
var initialLoginPromptShown bool = false

// loadEnvFile loads environment variables from a file
func loadEnvFile(envFilePath string) error {
	if envFilePath == "" {
		return nil // No env file specified
	}

	fmt.Printf("Loading environment variables from: %s\n", envFilePath)

	file, err := os.Open(envFilePath)
	if err != nil {
		return fmt.Errorf("failed to open env file %s: %v", envFilePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			fmt.Printf("Warning: Invalid format on line %d: %s\n", lineNum, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) ||
			(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			value = value[1 : len(value)-1]
		}

		// Set environment variable
		if err := os.Setenv(key, value); err != nil {
			fmt.Printf("Warning: Failed to set environment variable %s: %v\n", key, err)
		} else {
			fmt.Printf("Loaded env var: %s\n", key)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading env file: %v", err)
	}

	return nil
}

func main() {
	// Define command-line flags
	var filePath string
	var cdpbrowserPath string
	var envFilePath string
	flag.StringVar(&filePath, "file", "", "Path to a file whose content will be sent to OpenAI")
	flag.StringVar(&cdpbrowserPath, "cdpbrowser", "../server/cdpbrowser/cdpbrowser", "Path to the cdpbrowser server executable")
	flag.StringVar(&envFilePath, "env", "", "Path to environment file containing API keys (e.g., .vscode/voicebrowser.env)")
	flag.Parse()

	// Load environment variables from file if specified
	if err := loadEnvFile(envFilePath); err != nil {
		log.Fatalf("Failed to load environment file: %v", err)
	}

	// Show updated usage information
	fmt.Println("VoiceBrowser: OpenAI-powered browser automation using CDP browser server")
	fmt.Printf("Using cdpbrowser server: %s\n", cdpbrowserPath)
	if envFilePath != "" {
		fmt.Printf("Loaded environment from: %s\n", envFilePath)
	}

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(apiKey)

	// Initialize MCP connection to cdpbrowser server
	ctx := context.Background()
	cmd := exec.Command(cdpbrowserPath)
	client := mcp.NewClient(&mcp.Implementation{Name: "voicebrowser-client", Version: "v1.0.0"}, nil)

	fmt.Printf("Starting cdpbrowser server: %s\n", cdpbrowserPath)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		log.Fatalf("Failed to connect to cdpbrowser server: %v", err)
	}
	defer session.Close()

	// Store session globally for tool execution
	globalMCPSession = session

	fmt.Println("Connected to cdpbrowser server successfully")

	// Get available tools
	tools := listTools(ctx, session)

	// Verify cdpbrowser tools are available
	fmt.Println("\nVerifying cdpbrowser connection...")
	browserTools := verifyCDPBrowserTools(ctx, session)
	if len(browserTools) == 0 {
		log.Fatal("No cdpbrowser tools detected. Please ensure the cdpbrowser server is working correctly.")
	}

	fmt.Printf("cdpbrowser successfully connected! Found %d browser tools.\n", len(browserTools))
	// Use the browser tools for OpenAI interaction
	tools = browserTools

	// Prepare message for OpenAI
	var message string
	if filePath != "" {
		// Read the file content if file path is provided
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Error reading file %s: %v", filePath, err)
		}
		message = fmt.Sprintf("Here's a document that contains a numbered sequence of steps between {steps} and {/steps} delimiters, that require to be automated.\n\n{steps}%s{/steps}\n\nAnalyze one step at a time and return the next step to be performed. Think step by step. Use the cdpbrowser tools provided.\n", string(content))
		fmt.Printf("Using content from file: %s\n", filePath)
	} else {
		// Use default message focused on element discovery
		message = "Please demonstrate browser automation by going to Google.com, taking an ARIA snapshot to understand the page structure, then typing 'artificial intelligence' in the search box and clicking the search button. Show me how you use the ARIA snapshot to find the correct element selectors."
		fmt.Println("Using default demonstration message")
	}

	// Send request to OpenAI with verified browser tools
	resp, err := sendChatRequest(ctx, openaiClient, message, tools)
	if err != nil {
		log.Fatalf("Error calling OpenAI API: %v", err)
	}

	fmt.Println("\nOpenAI Response:")
	fmt.Println(resp)
}

// List available tools from the MCP server
func listTools(ctx context.Context, session *mcp.ClientSession) []*mcp.Tool {
	var tools []*mcp.Tool

	fmt.Println("Available tools:")
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			break // End of iteration
		}

		fmt.Printf("\t%s: %s\n", tool.Name, tool.Description)
		tools = append(tools, tool)
	}

	return tools
}

// Verify that cdpbrowser-specific tools are available
func verifyCDPBrowserTools(ctx context.Context, session *mcp.ClientSession) []*mcp.Tool {
	var cdpbrowserTools []*mcp.Tool

	// cdpbrowser tool names to look for
	cdpbrowserToolNames := []string{
		"navigate",
		"click",
		"screenshot",
		"aria_snapshot",
		"type_text",
		"click_button",
		"click_link",
		"select_dropdown",
		"choose_option",
		"refresh_page",
		"close_browser",
		"set_chrome_lifecycle",
		"shutdown_server",
	}

	fmt.Println("Looking for cdpbrowser tools:")
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			break // End of iteration
		}

		// Check if this is a cdpbrowser tool
		isCDPBrowserTool := false
		for _, toolName := range cdpbrowserToolNames {
			if tool.Name == toolName {
				isCDPBrowserTool = true
				break
			}
		}

		if isCDPBrowserTool {
			fmt.Printf("\t✓ Found: %s - %s\n", tool.Name, tool.Description)
			cdpbrowserTools = append(cdpbrowserTools, tool)
		}
	}

	if len(cdpbrowserTools) == 0 {
		fmt.Println("\tNo cdpbrowser tools found. Server may not be running properly.")
	}

	return cdpbrowserTools
}

// Convert MCP tools to OpenAI tool format
func convertToOpenAITools(mcpTools []*mcp.Tool) []openai.Tool {
	var tools []openai.Tool

	for _, t := range mcpTools {
		// Skip tools with missing schemas
		if t.InputSchema == nil {
			fmt.Printf("WARNING: Tool %s has nil InputSchema, skipping\n", t.Name)
			continue
		}

		// Convert the input schema to a map
		schemaBytes, err := json.Marshal(t.InputSchema)
		if err != nil {
			fmt.Printf("WARNING: Error marshaling schema for tool %s: %v\n", t.Name, err)
			continue
		}

		var schemaMap map[string]interface{}
		if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
			fmt.Printf("WARNING: Error unmarshaling schema for tool %s: %v\n", t.Name, err)
			continue
		}

		// Ensure the schema has the minimum required properties for OpenAI
		if schemaMap == nil {
			schemaMap = make(map[string]interface{})
		}

		// Check if type is missing and add it
		if _, ok := schemaMap["type"]; !ok {
			schemaMap["type"] = "object"
		}

		// Check if properties is missing and add it
		if _, ok := schemaMap["properties"]; !ok {
			schemaMap["properties"] = map[string]interface{}{}
		}

		// Create a proper description that encourages tool use
		description := t.Description
		if description == "" {
			description = fmt.Sprintf("Use this tool to %s", t.Name)
		}

		// Convert the tool to OpenAI format
		tool := openai.Tool{
			Type: "function",
			Function: &openai.FunctionDefinition{
				Name:        t.Name,
				Description: description,
				Parameters:  schemaMap,
			},
		}
		tools = append(tools, tool)
	}

	return tools
}

// Get MCP session helper function
func getMCPSession() *mcp.ClientSession {
	return globalMCPSession
}

// Send a chat request to OpenAI
func sendChatRequest(ctx context.Context, client *openai.Client, userMessage string, mcpTools []*mcp.Tool) (string, error) {
	// Get the MCP session for tool execution
	mcpSession := getMCPSession()
	if mcpSession == nil {
		return "", fmt.Errorf("MCP session not available for tool execution")
	}

	// Convert MCP tools to OpenAI format
	tools := convertToOpenAITools(mcpTools)

	// Debug: Print tool schemas to help diagnose issues
	if os.Getenv("DEBUG") == "1" {
		fmt.Println("Tool schemas being sent to OpenAI:")
		for i, tool := range tools {
			fmt.Printf("Tool %d: %s\n", i+1, tool.Function.Name)
			paramsJSON, _ := json.MarshalIndent(tool.Function.Parameters, "  ", "  ")
			fmt.Printf("  Parameters: %s\n\n", string(paramsJSON))
		}
	}

	// Keep track of all messages in the conversation
	messages := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: "You are an expert browser automation assistant using cdpbrowser MCP tools. " +
				"When the user asks you to interact with web pages, you MUST:\n" +
				"1. Use 'navigate' to go to websites\n" +
				"2. Use 'aria_snapshot' to understand page structure and find element selectors\n" +
				"3. Use element interaction tools (type_text, click_button, click_link, etc.) with the selectors you found\n" +
				"4. Use 'screenshot' to capture results when helpful\n\n" +
				"For element selection:\n" +
				"- CSS selectors like 'input[name=\"q\"]' for Google search\n" +
				"- ARIA selectors like 'button[aria-label=\"Search\"]'\n" +
				"- Text-based selectors like 'Submit' for buttons\n" +
				"- ID selectors like '#search-box'\n\n" +
				"CRITICAL: When analyzing ARIA snapshots, carefully scan ALL INTERACTIVE ELEMENTS for the exact text you need. " +
				"Look for buttons, links, and other elements that match the target text exactly. " +
				"For example, if looking for 'Canva AI', scan through the entire INTERACTIVE ELEMENTS section for buttons or links containing 'Canva AI'. " +
				"If you find the element, USE IT IMMEDIATELY - don't ignore it or claim it doesn't exist.\n\n" +
				"Always take an ARIA snapshot first to understand the page before interacting with elements. " +
				"Don't guess selectors - use the snapshot to find the correct ones. " +
				"When you find the target element in the snapshot, proceed with the action immediately.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userMessage,
		},
	}

	var finalResponse strings.Builder
	finalResponse.WriteString("Tool Execution Flow:\n\n")

	// Create a conversation loop for tool calls - continue until no more tool calls
	iteration := 0
	maxIterations := 50 // Safety limit to prevent infinite loops - can be increased if needed
	for iteration < maxIterations {
		iteration++
		// Sleep for a short duration to avoid hitting rate limits
		time.Sleep(2 * time.Second)

		// Create chat completion request with current messages
		req := openai.ChatCompletionRequest{
			Model:       openai.GPT4o,
			Messages:    messages,
			Tools:       tools,
			ToolChoice:  "auto", // Allow model to decide whether to use tools
			Temperature: 0.2,    // Lower temperature for more deterministic responses
		}

		// Dump full JSON request if DEBUG is enabled
		if os.Getenv("DEBUG") == "1" {
			requestJSON, _ := json.MarshalIndent(req, "", "  ")
			fmt.Printf("\n==== FULL OPENAI REQUEST (Iteration %d) ====\n%s\n==== END REQUEST ====\n\n",
				iteration, string(requestJSON))
		}

		// Call OpenAI API with rate limit handling
		var resp openai.ChatCompletionResponse
		var err error
		maxRetries := 5
		backoffDuration := 2 * time.Second

		for retryCount := 0; retryCount < maxRetries; retryCount++ {
			resp, err = client.CreateChatCompletion(ctx, req)

			if err == nil {
				// Success, break out of retry loop
				break
			}

			// Check if it's a rate limit error
			if apiErr, ok := err.(*openai.APIError); ok && (apiErr.Type == "rate_limit_exceeded" || apiErr.Code == "rate_limit_exceeded") {
				retryAfter := backoffDuration * time.Duration(retryCount+1)
				fmt.Printf("Rate limit exceeded. Retrying in %v (attempt %d/%d)...\n",
					retryAfter, retryCount+1, maxRetries)
				time.Sleep(retryAfter)
				continue
			}

			// Not a rate limit error, break and return the error
			break
		}

		if err != nil {
			// If we get an error, try to extract more details
			if apiErr, ok := err.(*openai.APIError); ok {
				return "", fmt.Errorf("OpenAI API error: Type=%s, Code=%s, Message=%s",
					apiErr.Type, apiErr.Code, apiErr.Message)
			}
			return "", err
		}

		// Dump the full response JSON if DEBUG is enabled
		if os.Getenv("DEBUG") == "1" {
			respJSON, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Printf("\n==== FULL OPENAI RESPONSE (Iteration %d) ====\n%s\n==== END RESPONSE ====\n\n",
				iteration, string(respJSON))
		}

		// Process the response
		choice := resp.Choices[0]
		finalResponse.WriteString(fmt.Sprintf("**Iteration %d:**\n", iteration))
		finalResponse.WriteString(fmt.Sprintf("OpenAI: %s\n\n", choice.Message.Content))

		// Add assistant's message to conversation
		messages = append(messages, choice.Message)

		// Check if the model wants to call tools
		if len(choice.Message.ToolCalls) == 0 {
			// No tool calls, but model may have provided final response
			fmt.Printf("OpenAI completed without tool calls. Response: %s\n", choice.Message.Content)
			break
		}

		// Execute tool calls
		for _, toolCall := range choice.Message.ToolCalls {
			fmt.Printf("Executing tool: %s\n", toolCall.Function.Name)
			finalResponse.WriteString(fmt.Sprintf("Executing tool: %s\n", toolCall.Function.Name))

			// Execute the MCP tool
			result, err := executeMCPTool(ctx, mcpSession, toolCall.Function.Name, toolCall.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
				fmt.Printf("Tool execution error: %v\n", err)
			}

			fmt.Printf("Tool result: %s\n\n", result)
			finalResponse.WriteString(fmt.Sprintf("Result: %s\n\n", result))

			// Check if this was the first navigate to the target website - if so, pause for manual login/cleanup
			if toolCall.Function.Name == "navigate" && !initialLoginPromptShown {
				// Parse the arguments to see if this is navigating to the target website
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
					if url, ok := args["url"].(string); ok {
						// Check if this is a target website (not just any navigation)
						if strings.Contains(strings.ToLower(url), "canva.com") {
							fmt.Printf("\n🌐 Navigation to target website completed. Pausing for manual intervention...\n")
							fmt.Println("Please complete any necessary login to Canva and close any popup dialogues that may impede the workflow.")
							fmt.Print("Press Enter when ready to continue automation: ")

							// Wait for user input
							reader := bufio.NewReader(os.Stdin)
							reader.ReadLine()

							fmt.Println("✅ Continuing automation...")
							initialLoginPromptShown = true
						}
					}
				}
			}

			// Add tool result to conversation
			toolMessage := openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    result,
				ToolCallID: toolCall.ID,
			}
			messages = append(messages, toolMessage)
		}

		// Add a 30-second delay between steps to avoid rate limits
		if len(choice.Message.ToolCalls) > 0 {
			fmt.Printf("\n⏱️  Waiting 30 seconds to avoid rate limits...\n")
			time.Sleep(30 * time.Second)
			fmt.Printf("✅ Continuing to next step...\n\n")
		}

		// Continue to next iteration for model to process tool results
	}

	// Check if we hit the safety limit
	if iteration >= maxIterations {
		finalResponse.WriteString(fmt.Sprintf("\n**Reached maximum iterations (%d). Stopping for safety.**\n", maxIterations))
		fmt.Printf("Warning: Reached maximum iterations (%d). Consider increasing the limit if more automation is needed.\n", maxIterations)
	}

	return finalResponse.String(), nil
}

// Execute an MCP tool with the given name and arguments
func executeMCPTool(ctx context.Context, mcpSession *mcp.ClientSession, toolName string, argsJSON string) (string, error) {
	if mcpSession == nil {
		return "", fmt.Errorf("MCP session is not available")
	}

	// Parse the arguments JSON
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("failed to parse tool arguments: %v", err)
	}

	// Execute the tool
	result, err := mcpSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})

	if err != nil {
		return "", fmt.Errorf("failed to execute tool %s: %v", toolName, err)
	}

	// Convert result to string
	var resultText strings.Builder
	for _, content := range result.Content {
		switch c := content.(type) {
		case *mcp.TextContent:
			resultText.WriteString(c.Text)
		case *mcp.ImageContent:
			resultText.WriteString(fmt.Sprintf("[Image: %s, %d bytes]", c.MIMEType, len(c.Data)))
		default:
			resultText.WriteString(fmt.Sprintf("[Unknown content type: %T]", content))
		}
	}

	return resultText.String(), nil
}
