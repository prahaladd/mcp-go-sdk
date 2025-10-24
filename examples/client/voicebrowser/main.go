// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// The autobrowser command connects to an MCP server and uses OpenAI API for interaction.
//
// Usage: autobrowser <command> [<args>]
//
// For example:
//
//	autobrowser go run github.com/modelcontextprotocol/go-sdk/examples/server/hello
//
// or
//
//	autobrowser npx browsermcp/mcp@latest
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// CommandInvocation represents a successful MCP tool invocation
type CommandInvocation struct {
	ToolName  string                 `json:"tool_name"`
	Arguments map[string]interface{} `json:"arguments"`
	Result    string                 `json:"result"`
	Timestamp string                 `json:"timestamp"`
}

// LearningSummary stores all successful command invocations
type LearningSummary struct {
	Commands []CommandInvocation `json:"commands"`
}

// Global variable to store successful command invocations
var learningData LearningSummary

// Path to the temporary directory for storing individual command logs
var tempCommandDir string

func main() {
	// Define command-line flags
	var filePath string
	flag.StringVar(&filePath, "file", "", "Path to a file whose content will be sent to OpenAI")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: autobrowser [--file=<path>] <command> [<args>]\n")
		fmt.Fprintf(os.Stderr, "Connects to an MCP server and uses OpenAI API for interaction\n")
		fmt.Fprintf(os.Stderr, "Example: autobrowser npx browsermcp/mcp@latest\n")
		fmt.Fprintf(os.Stderr, "Example with file: autobrowser --file=./myfile.txt npx browsermcp/mcp@latest\n")
		os.Exit(2)
	}

	// Get OpenAI API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(apiKey)

	// Initialize MCP connection
	ctx := context.Background()
	cmd := exec.Command(args[0], args[1:]...)
	client := mcp.NewClient(&mcp.Implementation{Name: "autobrowser-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer session.Close()

	// Store session globally for tool execution
	globalMCPSession = session

	fmt.Println("Connected to MCP server successfully")

	// Get available tools
	tools := listTools(ctx, session)

	// Prompt user to connect browser
	fmt.Println("\n=== IMPORTANT ===")
	fmt.Println("Please connect your browser to the MCP server now.")
	fmt.Println("Once connected, press Enter to continue...")
	fmt.Print("> ")

	// Wait for user input
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	fmt.Println("Continuing with browser automation...")

	// Verify browser tools are available
	fmt.Println("\nVerifying browser connection...")
	browserTools := verifyBrowserTools(ctx, session)
	if len(browserTools) == 0 {
		fmt.Println("Warning: No browser tools detected. The browser might not be properly connected.")
		fmt.Println("Do you want to continue anyway? (y/n)")
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "y" && input != "Y" {
			fmt.Println("Exiting. Please try again after connecting the browser.")
			os.Exit(0)
		}
		// Use the original tools if user wants to continue
		browserTools = tools
	}

	// Use the original tools if user wants to continue
	if len(browserTools) == 0 {
		browserTools = tools
	} else {
		fmt.Printf("Browser successfully connected! Found %d browser tools.\n", len(browserTools))
		// Update tools list with the current browser tools
		tools = browserTools
	}

	// Prepare message for OpenAI
	var message string
	if filePath != "" {
		// Read the file content if file path is provided
		content, err := os.ReadFile(filePath)
		if err != nil {
			log.Fatalf("Error reading file %s: %v", filePath, err)
		}
		message = fmt.Sprintf("Here's a document that contains a numbered sequence of steps between {steps} and {/steps} delimiters, that require to be automated.\n\n{steps}%s{/steps}\n\nAnalyze one step at a time and return the next step to be performed.Think step by step. Restrict the tools to the  list provided in the request.\n", string(content))
		fmt.Printf("Using content from file: %s\n", filePath)
	} else {
		// Use default message if no file path is provided
		message = "Please demonstrate how to use the browser tools by navigating to a website about artificial intelligence and taking a screenshot. Use the tools provided rather than just telling me what you would do."
		fmt.Println("Using default message")
	}

	// Initialize the learning data
	learningData = LearningSummary{Commands: []CommandInvocation{}}

	// Create a temporary directory for command logs
	tempCommandDir = os.TempDir() + "/mcp-commands-" + time.Now().Format("20060102-150405")
	errDir := os.MkdirAll(tempCommandDir, 0755)
	if errDir != nil {
		fmt.Printf("Warning: Failed to create temporary directory for command logs: %v\n", errDir)
		tempCommandDir = "" // Disable individual command logging
	} else {
		fmt.Printf("Command logs will be stored in: %s\n", tempCommandDir)
	}

	// Check for and execute commands from learning.json file
	learningFile := "learning.json"
	executed, err := executeStoredCommands(ctx, session, learningFile)
	if err != nil {
		fmt.Printf("Error executing stored commands: %v\n", err)
		// Continue with normal operation
	}

	// If we successfully executed stored commands, ask if the user wants to proceed with OpenAI
	if executed {
		fmt.Println("\nDo you want to proceed with OpenAI API interaction? (y/n)")
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input != "y" && input != "Y" {
			fmt.Println("Exiting. Stored commands have been executed.")
			// Save the learning data from replay to learning.json
			saveLearningData(learningFile)
			os.Exit(0)
		}
		fmt.Println("Continuing with OpenAI interaction...")
	}

	// Send request to OpenAI with verified browser tools
	resp, err := sendChatRequest(ctx, openaiClient, message, tools)
	if err != nil {
		log.Fatalf("Error calling OpenAI API: %v", err)
	}

	fmt.Println("\nOpenAI Response:")
	fmt.Println(resp)

	// Save the learning data to a file
	saveLearningData("learning.json")
}

// Save the learning data to a JSON file
func saveLearningData(filename string) {
	// If we have a temp directory, try to recover any commands from there
	// that might not be in memory (e.g., if the program crashed and restarted)
	if tempCommandDir != "" && len(learningData.Commands) == 0 {
		recoverCommandsFromFiles()
	}

	fmt.Printf("\nSaving %d command invocations to %s...\n", len(learningData.Commands), filename)

	// Marshal the learning data to JSON
	jsonData, err := json.MarshalIndent(learningData, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling learning data: %v\n", err)
		return
	}

	// Write the JSON to a file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		fmt.Printf("Error writing learning data to file: %v\n", err)
		return
	}

	fmt.Printf("Successfully saved learning data to %s\n", filename)
}

// recoverCommandsFromFiles loads any command logs from the temp directory
func recoverCommandsFromFiles() {
	if tempCommandDir == "" {
		return
	}

	fmt.Println("Attempting to recover commands from individual log files...")

	// Read all files in the temp directory
	files, err := os.ReadDir(tempCommandDir)
	if err != nil {
		fmt.Printf("Error reading command log directory: %v\n", err)
		return
	}

	// Process each file
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// Read the file
		data, err := os.ReadFile(fmt.Sprintf("%s/%s", tempCommandDir, file.Name()))
		if err != nil {
			fmt.Printf("Error reading command log file %s: %v\n", file.Name(), err)
			continue
		}

		// Unmarshal the JSON
		var cmd CommandInvocation
		if err := json.Unmarshal(data, &cmd); err != nil {
			fmt.Printf("Error unmarshaling command log file %s: %v\n", file.Name(), err)
			continue
		}

		// Add to the learning data if not already present
		isDuplicate := false
		for _, existingCmd := range learningData.Commands {
			if existingCmd.Timestamp == cmd.Timestamp && existingCmd.ToolName == cmd.ToolName {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			learningData.Commands = append(learningData.Commands, cmd)
			fmt.Printf("Recovered command: %s from file %s\n", cmd.ToolName, file.Name())
		}
	}

	fmt.Printf("Command recovery complete. Total commands: %d\n", len(learningData.Commands))
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

// Verify that browser-specific tools are available
func verifyBrowserTools(ctx context.Context, session *mcp.ClientSession) []*mcp.Tool {
	var browserTools []*mcp.Tool

	// Common browser tool name prefixes to look for
	browserPrefixes := []string{
		"browser_navigate",
		"browser_click",
		"browser_type",
		"browser_snapshot",
		"mcp_browser",
	}

	fmt.Println("Looking for browser tools:")
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			break // End of iteration
		}

		// Check if this is a browser-related tool
		isBrowserTool := false
		for _, prefix := range browserPrefixes {
			if strings.HasPrefix(tool.Name, prefix) {
				isBrowserTool = true
				break
			}
		}

		if isBrowserTool {
			fmt.Printf("\t✓ Found: %s\n", tool.Name)
			browserTools = append(browserTools, tool)
		}
	}

	if len(browserTools) == 0 {
		fmt.Println("\tNo browser tools found. Browser may not be connected properly.")
	}

	return browserTools
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
		// OpenAI requires at least type and properties fields
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
			Content: "You are an assistant that helps users navigate and control a browser using MCP tools. " +
				"You MUST use the browser tools provided to you when the user asks for anything related to browsing. " +
				"Don't just describe what you would do - actually use the tools.",
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userMessage + " (Please use the browser tools provided to help with this task)",
		},
	}

	// Maximum number of iterations to prevent infinite loops
	maxIterations := 5
	var finalResponse strings.Builder
	finalResponse.WriteString("Tool Execution Flow:\n\n")

	// Create a conversation loop for tool calls
	for iteration := 0; iteration < maxIterations; iteration++ {
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
				iteration+1, string(requestJSON))
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
				iteration+1, string(respJSON))
		}

		// Check for valid response
		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("no response from OpenAI")
		}

		// Get the assistant's message
		assistantMsg := resp.Choices[0].Message

		// Add the assistant's message to our conversation
		messages = append(messages, openai.ChatCompletionMessage{
			Role:      openai.ChatMessageRoleAssistant,
			Content:   assistantMsg.Content,
			ToolCalls: assistantMsg.ToolCalls,
		})

		// Add to final response
		if assistantMsg.Content != "" {
			finalResponse.WriteString(fmt.Sprintf("Assistant (Step %d): %s\n\n", iteration+1, assistantMsg.Content))
		}

		// Check if there are any tool calls
		if len(assistantMsg.ToolCalls) == 0 {
			// No tool calls, so we're done
			fmt.Println("No more tool calls, finishing conversation.")
			break
		}

		fmt.Printf("\nExecuting %d tool calls from OpenAI (Iteration %d):\n",
			len(assistantMsg.ToolCalls), iteration+1)

		// Execute each tool call and add results to messages
		for i, toolCall := range assistantMsg.ToolCalls {
			toolName := toolCall.Function.Name
			toolArgs := toolCall.Function.Arguments
			toolCallID := toolCall.ID

			fmt.Printf("Tool %d: %s\nArguments: %s\n", i+1, toolName, toolArgs)
			finalResponse.WriteString(fmt.Sprintf("Tool Call %d-%d: %s\nArguments: %s\n",
				iteration+1, i+1, toolName, toolArgs))

			// Execute the tool and get results
			result, err := executeMCPTool(ctx, mcpSession, toolName, toolArgs)

			// Format the result for both display and message
			var resultMsg string
			if err != nil {
				resultMsg = fmt.Sprintf("Error executing %s: %v", toolName, err)
			} else {
				resultMsg = result
			}

			// Display the result
			fmt.Printf("Result: %s\n\n", resultMsg)
			finalResponse.WriteString(fmt.Sprintf("Result: %s\n\n", resultMsg))

			// Add tool result as a message
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool, // This is the role for tool results
				Content:    resultMsg,
				ToolCallID: toolCallID,
			})
		}

		// Check if we've reached the max iterations
		if iteration == maxIterations-1 {
			finalResponse.WriteString("\nReached maximum number of iterations. Stopping.\n")
		}
	}

	// Return the final compiled response
	return finalResponse.String(), nil
}

// Get the globally stored MCP session
func getMCPSession() *mcp.ClientSession {
	return globalMCPSession
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

	// Use the common function to execute the tool
	result, err := executeMCPToolWithArgs(ctx, mcpSession, toolName, args)
	if err != nil {
		return "", err
	}

	// Store the successful command invocation
	invocation := CommandInvocation{
		ToolName:  toolName,
		Arguments: args,
		Result:    result,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	learningData.Commands = append(learningData.Commands, invocation)

	// Also persist to a file immediately for safety
	if tempCommandDir != "" {
		saveCommandToFile(invocation)
	}

	return result, nil
}

// saveCommandToFile saves a single command invocation to a unique file
func saveCommandToFile(cmd CommandInvocation) {
	// Create a unique filename based on timestamp and tool name
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("%s/%d-%s.json", tempCommandDir, timestamp, cmd.ToolName)

	// Marshal the command to JSON
	jsonData, err := json.MarshalIndent(cmd, "", "  ")
	if err != nil {
		fmt.Printf("Warning: Failed to marshal command data: %v\n", err)
		return
	}

	// Write the JSON to a file
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		fmt.Printf("Warning: Failed to write command data to file: %v\n", err)
		return
	}

	if os.Getenv("DEBUG") == "1" {
		fmt.Printf("Command log saved to: %s\n", filename)
	}
}

// calculateHash computes a SHA-256 hash of a string
func calculateHash(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// executeStoredCommands attempts to execute commands from a previously saved learning.json file
func executeStoredCommands(ctx context.Context, session *mcp.ClientSession, filename string) (bool, error) {
	// Check if the file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("No previous %s found, skipping command replay\n", filename)
		return false, nil
	}

	// Read the file
	data, err := os.ReadFile(filename)
	if err != nil {
		return false, fmt.Errorf("error reading %s: %v", filename, err)
	}

	// Unmarshal the JSON
	var summary LearningSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return false, fmt.Errorf("error parsing %s: %v", filename, err)
	}

	if len(summary.Commands) == 0 {
		fmt.Printf("No commands found in %s\n", filename)
		return false, nil
	}

	fmt.Printf("\n=== REPLAYING %d COMMANDS FROM %s ===\n\n", len(summary.Commands), filename)

	// Create a new learning data structure for updated results
	updatedLearning := LearningSummary{Commands: []CommandInvocation{}}

	// Execute each command
	for i, cmd := range summary.Commands {
		fmt.Printf("Executing command %d/%d: %s\n", i+1, len(summary.Commands), cmd.ToolName)
		fmt.Printf("Arguments: %v\n", cmd.Arguments)

		// Execute the tool
		result, err := executeMCPToolWithArgs(ctx, session, cmd.ToolName, cmd.Arguments)
		if err != nil {
			fmt.Printf("Error executing %s: %v\n", cmd.ToolName, err)
			// Add the original command to the updated learning data
			updatedLearning.Commands = append(updatedLearning.Commands, cmd)
			continue
		}
		// convert the result string to JSON dictionary and check if
		// isError field is present and set to true
		var resultMap map[string]interface{}
		if err := json.Unmarshal([]byte(result), &resultMap); err == nil {
			if isError, ok := resultMap["isError"].(bool); ok && isError {
				fmt.Printf("Tool execution resulted in an error: %s\n", result)
				// take the contents of the snapshot from the previous command present in the updated summary
				// if available and try again. the arguments to the current command should
				// be updated based on the new snapshot analysis by call to OpenAI.
				// Pass the current command arguments to OpenAI to help it understand
				// the tool arguments
				if len(updatedLearning.Commands) > 0 {
					lastCmd := updatedLearning.Commands[len(updatedLearning.Commands)-1]
					if lastCmdResult := lastCmd.Result; lastCmdResult != "" {
						// Create a new OpenAI client for analysis
						openaiClient := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

						// Create chat request to analyze snapshot and suggest new arguments
						analysisReq := openai.ChatCompletionRequest{
							Model: openai.GPT4o,
							Messages: []openai.ChatCompletionMessage{
								{
									Role:    openai.ChatMessageRoleSystem,
									Content: "Analyze the content of the current command snapshot  and the previous command's updated snapshot. Compare them and suggest new arguments for the failed command to help it succeed. Return ONLY a JSON dictionary with the new arguments, no other text. The structure of the dictionary should be exactly as required by the current tool's arguments.",
								},
								{
									Role: openai.ChatMessageRoleUser,
									Content: fmt.Sprintf("Previous command updated snapshot: %s\nFailed command: %s\nCurrent tool snapshot: %s\nCurrent tool arguments: %s",
										lastCmdResult, cmd.ToolName, cmd.Result, cmd.Arguments),
								},
							},
						}

						// Get analysis from OpenAI
						analysisResp, err := openaiClient.CreateChatCompletion(ctx, analysisReq)
						if err == nil && len(analysisResp.Choices) > 0 {
							// Try to parse the suggestion into new arguments
							suggestion := analysisResp.Choices[0].Message.Content
							var newArgs map[string]interface{}
							if err := json.Unmarshal([]byte(suggestion), &newArgs); err == nil {
								// Retry the command with new arguments
								if retryResult, err := executeMCPToolWithArgs(ctx, session, cmd.ToolName, newArgs); err == nil {
									result = retryResult
								}
							}
						}
					}
				}
			}
		}

		// Calculate hashes for comparison
		originalHash := calculateHash(cmd.Result)
		newHash := calculateHash(result)

		// Create a new command invocation with updated timestamp
		updatedCmd := CommandInvocation{
			ToolName:  cmd.ToolName,
			Arguments: cmd.Arguments,
			Timestamp: time.Now().Format(time.RFC3339),
		}

		if originalHash == newHash {
			fmt.Printf("Result matches the previous execution, continuing\n")
			updatedCmd.Result = cmd.Result // Keep the original result
		} else {
			fmt.Printf("Result differs from previous execution, updating\n")
			updatedCmd.Result = result // Use the new result
		}

		// Add to the updated learning data
		updatedLearning.Commands = append(updatedLearning.Commands, updatedCmd)

		// Add a brief pause between commands
		time.Sleep(500 * time.Millisecond)
	}

	fmt.Printf("\n=== COMMAND REPLAY COMPLETE ===\n\n")

	// Save the updated learning data
	updatedFilename := filename + ".updated"
	jsonData, err := json.MarshalIndent(updatedLearning, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling updated learning data: %v\n", err)
	} else {
		err = os.WriteFile(updatedFilename, jsonData, 0644)
		if err != nil {
			fmt.Printf("Error writing updated learning data to %s: %v\n", updatedFilename, err)
		} else {
			fmt.Printf("Updated learning data saved to %s\n", updatedFilename)
		}
	}

	// Set the global learning data to the updated version
	learningData = updatedLearning

	return true, nil
} // executeMCPToolWithArgs executes an MCP tool with the given name and argument map
func executeMCPToolWithArgs(ctx context.Context, mcpSession *mcp.ClientSession, toolName string, args map[string]interface{}) (string, error) {
	if mcpSession == nil {
		return "", fmt.Errorf("MCP session is not available")
	}

	fmt.Printf("Executing MCP tool: %s with args: %v\n", toolName, args)

	// Find the tool by name
	var targetTool *mcp.Tool
	for tool, err := range mcpSession.Tools(ctx, nil) {
		if err != nil {
			break
		}
		if tool.Name == toolName {
			targetTool = tool
			break
		}
	}

	if targetTool == nil {
		return "", fmt.Errorf("tool not found: %s", toolName)
	}

	// Execute the tool
	result, err := mcpSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
	if err != nil {
		return "", fmt.Errorf("failed to execute tool: %v", err)
	}

	// Convert result to string representation
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %v", err)
	}

	return string(resultJSON), nil
}
