// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// The voicebrowser command connects to cdpbrowser server and uses OpenAI API for browser automation.
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
	"regexp"
	"strings"
	"time"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	defaultef "github.com/amikos-tech/chroma-go/pkg/embeddings/default_ef"
	"github.com/modelcontextprotocol/go-sdk/examples/client/voicebrowser/chromadb"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	openai "github.com/sashabaranov/go-openai"
)

// Global MCP session for tool execution
var globalMCPSession *mcp.ClientSession

// Global ChromaDB state
var (
	globalChromaClient     *chromadb.Client
	globalChromaCollection chroma.Collection
	globalSessionID        string
	currentPageURL         string
	totalElementsStored    int
)

// Global flag to track if initial login prompt has been shown
var initialLoginPromptShown bool = false

// AriaElement represents a parsed HTML element from ARIA snapshot
type AriaElement struct {
	ElementType     string
	DisplayText     string
	AriaLabel       string
	PrimarySelector string
	AltSelector     string
}

// Step represents a parsed execution step for ChromaDB-driven workflow
type Step struct {
	ToolName     string
	Description  string
	OriginalLine string
}

// generateSessionID creates a unique session identifier
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().Unix())
}

// generateElementID creates a unique ID for an ARIA element
func generateElementID(url, elementType, displayText, timestamp string, index int) string {
	data := url + ":" + elementType + ":" + displayText + ":" + timestamp + ":" + fmt.Sprintf("%d", index)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// parseAriaSnapshot parses ARIA snapshot text and extracts elements
func parseAriaSnapshot(ariaText string) ([]AriaElement, error) {
	var elements []AriaElement

	// Split by bullet points
	lines := strings.Split(ariaText, "•")

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Extract element type [button], [link], etc.
		// Handles both "[Type]" and "[Scope] [Type]"
		// Group 1: First bracket, Group 2: Second bracket (optional), Group 3: Display text
		typeRe := regexp.MustCompile(`(\[[^\]]+\])\s*(?:(\[[^\]]+\])\s*)?"([^"]+)"`)
		typeMatch := typeRe.FindStringSubmatch(line)
		
		elementType := ""
		displayText := ""
		
		if typeMatch != nil {
			if typeMatch[2] != "" {
				// Two brackets: [Scope] [Type]
				elementType = strings.Trim(typeMatch[2], "[]")
			} else {
				// One bracket: [Type]
				elementType = strings.Trim(typeMatch[1], "[]")
			}
			displayText = typeMatch[3]
		} else {
			// Fallback for lines that might not match the full pattern
			simpleRe := regexp.MustCompile(`\[([^\]]+)\]`)
			if m := simpleRe.FindStringSubmatch(line); m != nil {
				elementType = m[1]
			}
			displayRe := regexp.MustCompile(`"([^"]+)"`)
			if m := displayRe.FindStringSubmatch(line); m != nil {
				displayText = m[1]
			}
		}

		if elementType == "" || displayText == "" {
			continue
		}

		// Extract aria-label
		ariaRe := regexp.MustCompile(`aria-label:\s*"([^"]+)"`)
		ariaMatch := ariaRe.FindStringSubmatch(line)
		ariaLabel := ""
		if ariaMatch != nil {
			ariaLabel = ariaMatch[1]
		}

		// Extract primary selector
		// Handles: " (selector: .class-name)" or " (selector: input[type="text"])"
		primaryRe := regexp.MustCompile(`(?i)selector:\s*(.+?)(?:\s*\)|$)`)
		primaryMatch := primaryRe.FindStringSubmatch(line)
		primarySelector := ""
		if primaryMatch != nil {
			primarySelector = strings.TrimSpace(primaryMatch[1])
			// If it ends with a bracket from the outer (selector: ...), trim it
			// but be careful not to trim a bracket that belongs to the selector itself
			if strings.HasSuffix(primarySelector, ")") && strings.Count(primarySelector, "(") < strings.Count(primarySelector, ")") {
				primarySelector = strings.TrimSuffix(primarySelector, ")")
			}
		}

		// Extract alternative selector
		altRe := regexp.MustCompile(`(?i)Alternative selectors?:\s*([^\n\r]+)`)
		altMatch := altRe.FindStringSubmatch(line)
		altSelector := ""
		if altMatch != nil {
			altSelector = strings.TrimSpace(altMatch[1])
		}

		elements = append(elements, AriaElement{
			ElementType:     elementType,
			DisplayText:     displayText,
			AriaLabel:       ariaLabel,
			PrimarySelector: primarySelector,
			AltSelector:     altSelector,
		})
	}

	return elements, nil
}

// initializeChromaDB creates ChromaDB client and collection
func initializeChromaDB(ctx context.Context, chromaURL string) (*chromadb.Client, chroma.Collection, error) {
	// Generate unique session ID
	sessionID := generateSessionID()
	globalSessionID = sessionID

	// Create ChromaDB client
	client, err := chromadb.NewClient(ctx, chromaURL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to ChromaDB at %s: %w", chromaURL, err)
	}

	// Create embedding function
	ef, _, err := defaultef.NewDefaultEmbeddingFunction()
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to create embedding function: %w", err)
	}

	// Create collection
	collectionName := fmt.Sprintf("voicebrowser-session-%s", sessionID)
	collection, err := client.CreateCollection(collectionName, ef)
	if err != nil {
		client.Close()
		return nil, nil, fmt.Errorf("failed to create collection %s: %w", collectionName, err)
	}

	return client, collection, nil
}

// persistAriaSnapshot parses and stores ARIA snapshot in ChromaDB
func persistAriaSnapshot(ctx context.Context, ariaData string, url string) error {
	startTime := time.Now()

	// Try to extract URL from snapshot header if present
	// Format 1: [Page Structure] - https://example.com (Legacy)
	// Format 2: ARIA SNAPSHOT (URL: https://example.com) (New)
	urlRegex := regexp.MustCompile(`(?:\[Page Structure\] - |ARIA SNAPSHOT \(URL: )(https?://[^\s\n\)]+)`)
	if matches := urlRegex.FindStringSubmatch(ariaData); len(matches) > 1 {
		url = matches[1]
		currentPageURL = url // Update global state too
		fmt.Printf("📍 URL updated from snapshot: %s\n", url)
	}

	// Parse ARIA snapshot
	elements, err := parseAriaSnapshot(ariaData)
	if err != nil {
		return fmt.Errorf("failed to parse ARIA snapshot: %w", err)
	}

	if len(elements) == 0 {
		log.Printf("DEBUG: parseAriaSnapshot failed to parse any elements. Raw text length: %d", len(ariaData))
		if len(ariaData) > 0 {
			log.Printf("DEBUG: Raw text start: %s", truncateString(ariaData, 200))
		}
		return fmt.Errorf("no elements parsed from ARIA snapshot")
	}

	// Build ChromaDB documents
	timestamp := time.Now().UTC().Format(time.RFC3339)
	documents := make([]string, len(elements))
	metadatas := make([]chroma.DocumentMetadata, len(elements))
	ids := make([]string, len(elements))

	for i, elem := range elements {
		// Document: display text for semantic search
		documents[i] = elem.DisplayText

		// Metadata: all element details
		metadatas[i] = chroma.NewMetadata(
			chroma.NewStringAttribute("element_type", elem.ElementType),
			chroma.NewStringAttribute("aria_label", elem.AriaLabel),
			chroma.NewStringAttribute("primary_selector", elem.PrimarySelector),
			chroma.NewStringAttribute("alt_selector", elem.AltSelector),
			chroma.NewStringAttribute("url", url),
			chroma.NewStringAttribute("timestamp", timestamp),
			chroma.NewStringAttribute("session_id", globalSessionID),
		)

		// ID: hash of URL + element details + timestamp + index (ensures uniqueness)
		ids[i] = generateElementID(url, elem.ElementType, elem.DisplayText, timestamp, i)
	}

	// Add to ChromaDB - SYNCHRONOUS
	err = globalChromaClient.AddDocuments(globalChromaCollection, documents, metadatas, ids)
	if err != nil {
		return fmt.Errorf("failed to add documents to ChromaDB: %w", err)
	}

	// Update session metrics
	totalElementsStored += len(elements)

	duration := time.Since(startTime)
	fmt.Printf("✓ Persisted %d ARIA elements from %s (took %v)\n",
		len(elements), url, duration.Round(time.Millisecond))
	fmt.Printf("📊 Session total: %d elements stored\n", totalElementsStored)

	return nil
}

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
	var chromaDBURL string
	var enableChromaDB bool
	var executionMode string
	flag.StringVar(&filePath, "file", "", "Path to a file whose content will be sent to OpenAI")
	flag.StringVar(&cdpbrowserPath, "cdpbrowser", "../server/cdpbrowser/cdpbrowser", "Path to the cdpbrowser server executable")
	flag.StringVar(&envFilePath, "env", "", "Path to environment file containing API keys (e.g., .vscode/voicebrowser.env)")
	flag.StringVar(&chromaDBURL, "chromadb", "http://localhost:8000", "ChromaDB server URL")
	flag.BoolVar(&enableChromaDB, "enable-chromadb", true, "Enable ChromaDB persistence (default: true)")
	flag.StringVar(&executionMode, "execution-mode", "llm-driven", "Execution mode: 'llm-driven' (default) or 'chromadb-driven'")
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

	// Initialize ChromaDB if enabled
	if enableChromaDB {
		fmt.Printf("\nInitializing ChromaDB connection...\n")
		chromaClient, collection, err := initializeChromaDB(ctx, chromaDBURL)
		if err != nil {
			log.Fatalf("FATAL: ChromaDB initialization failed: %v\nPlease ensure ChromaDB is running at %s", err, chromaDBURL)
		}
		globalChromaClient = chromaClient
		globalChromaCollection = collection
		fmt.Printf("✓ ChromaDB connected: %s\n", chromaDBURL)
		fmt.Printf("✓ Collection: %s\n", collection.Name())

		// Cleanup on exit
		defer func() {
			if globalChromaClient != nil {
				fmt.Printf("\n✓ ChromaDB session collection: voicebrowser-session-%s\n", globalSessionID)
				fmt.Println("  (Collection persisted for future analysis)")
				globalChromaClient.Close()
			}
		}()
	} else {
		fmt.Println("\n⚠️  ChromaDB persistence disabled")
	}

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
		message = string(content)
		fmt.Printf("Using content from file: %s\n", filePath)
	} else {
		// Use default message focused on element discovery
		message = "Please demonstrate browser automation by going to Google.com, taking an ARIA snapshot to understand the page structure, then typing 'artificial intelligence' in the search box and clicking the search button. Show me how you use the ARIA snapshot to find the correct element selectors."
		fmt.Println("Using default demonstration message")
	}

	// Route to appropriate execution mode
	var resp string
	if executionMode == "chromadb-driven" {
		fmt.Println("\n🔄 Using ChromaDB-driven execution mode")
		resp, err = runChromaDBDrivenWorkflow(ctx, openaiClient, session, message, tools)
		if err != nil {
			log.Fatalf("Error in ChromaDB-driven workflow: %v", err)
		}
	} else {
		fmt.Println("\n🔄 Using LLM-driven execution mode")
		resp, err = runLLMDrivenWorkflow(ctx, openaiClient, message, tools)
		if err != nil {
			log.Fatalf("Error in LLM-driven workflow: %v", err)
		}
	}

	fmt.Println("\nWorkflow Response:")
	fmt.Println(resp)

	// Wait for user input before closing to allow viewing results
	fmt.Println("\n" + strings.Repeat("━", 70))
	fmt.Println("🏁 WORKFLOW FINISHED")
	fmt.Println(strings.Repeat("━", 70))
	fmt.Printf("The automation has completed all steps.\n")
	fmt.Printf("You can now inspect the browser window.\n")
	fmt.Print("→ Press ENTER to close the browser and exit: ")

	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	fmt.Println("👋 Exiting...")
}

// runLLMDrivenWorkflow executes the original LLM-driven automation workflow
func runLLMDrivenWorkflow(ctx context.Context, openaiClient *openai.Client, message string, tools []*mcp.Tool) (string, error) {
	// Original behavior - format message for step-by-step analysis if from file
	formattedMessage := message
	if strings.Contains(message, "\n") && !strings.Contains(message, "Please demonstrate") {
		formattedMessage = fmt.Sprintf("Here's a document that contains a numbered sequence of steps between {steps} and {/steps} delimiters, that require to be automated.\n\n{steps}%s{/steps}\n\nAnalyze one step at a time and return the next step to be performed. Think step by step. Use the cdpbrowser tools provided.\n", message)
	}

	// Send request to OpenAI with verified browser tools
	resp, err := sendChatRequest(ctx, openaiClient, formattedMessage, tools)
	if err != nil {
		return "", fmt.Errorf("error calling OpenAI API: %v", err)
	}

	return resp, nil
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
		"aria_snapshot_v2",
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

	// Add the workflow_complete pseudo-tool
	workflowCompleteTool := openai.Tool{
		Type: "function",
		Function: &openai.FunctionDefinition{
			Name:        "workflow_complete",
			Description: "Call this tool when the automation workflow is completely finished and no further actions are needed. This signals the end of the automation session.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "A message indicating the completion status and results",
					},
				},
				"required": []string{"message"},
			},
		},
	}
	tools = append(tools, workflowCompleteTool)

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
				"4. Use 'screenshot' to capture results when helpful\n" +
				"5. CALL 'workflow_complete' when the automation workflow is finished\n\n" +
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
				"When you find the target element in the snapshot, proceed with the action immediately.\n\n" +
				"WORKFLOW COMPLETION:\n" +
				"- When you have completed ALL required automation steps, call the 'workflow_complete' tool\n" +
				"- This signals the end of the automation session and prevents further unnecessary interactions\n" +
				"- Do NOT call workflow_complete until ALL steps in the instructions are finished\n" +
				"- Example completion scenarios: after taking final screenshot, after submitting forms, when workflow objectives are met",
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

			// Handle the workflow_complete pseudo-tool
			if toolCall.Function.Name == "workflow_complete" {
				// Parse the completion message
				var args map[string]interface{}
				var completionMessage string = "Workflow completed successfully"
				if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err == nil {
					if msg, ok := args["message"].(string); ok && msg != "" {
						completionMessage = msg
					}
				}

				fmt.Printf("\n🎯 WORKFLOW COMPLETE - %s\n", completionMessage)
				fmt.Printf("✅ All automation steps successfully executed. Session terminated.\n")
				finalResponse.WriteString(fmt.Sprintf("\n🎯 WORKFLOW COMPLETE - %s\n", completionMessage))
				finalResponse.WriteString("✅ All automation steps successfully executed. Session terminated.\n")

				// Add the workflow complete message to conversation
				toolMessage := openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    "Workflow completed successfully. Automation session terminated.",
					ToolCallID: toolCall.ID,
				}
				messages = append(messages, toolMessage)

				return finalResponse.String(), nil
			}

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
		fmt.Printf("\n⏱️  Waiting 30 seconds to avoid rate limits...\n")
		time.Sleep(30 * time.Second)

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

	resultString := resultText.String()

	// Persist ARIA snapshot to ChromaDB if enabled
	// We check if the result contains "ARIA SNAPSHOT" which indicates an ARIA snapshot
	// This handles both explicit snapshot tools and interaction tools that return snapshots
	if globalChromaClient != nil && (strings.Contains(resultString, "ARIA SNAPSHOT") || strings.Contains(resultString, "ARIA Snapshot")) {
		fmt.Printf("📊 Persisting latest ARIA snapshot to ChromaDB (from tool: %s)...\n", toolName)

		// SYNCHRONOUS persistence - block until complete
		if err := persistAriaSnapshot(ctx, resultString, currentPageURL); err != nil {
			// Log error but don't fail the tool execution
			// The ARIA data is still available in the result
			log.Printf("WARNING: Failed to persist ARIA snapshot: %v", err)
		}
	}

	// Track current URL for navigate calls
	if toolName == "navigate" {
		if url, ok := args["url"].(string); ok {
			currentPageURL = url
			fmt.Printf("📍 Current URL tracked: %s\n", url)
		}
	}

	return resultString, nil
}

// runChromaDBDrivenWorkflow executes workflow using ChromaDB for element selection
func runChromaDBDrivenWorkflow(ctx context.Context, openaiClient *openai.Client, session *mcp.ClientSession, userInstructions string, tools []*mcp.Tool) (string, error) {
	// Validate ChromaDB is available
	if globalChromaClient == nil {
		return "", fmt.Errorf("ChromaDB-driven mode requires ChromaDB to be enabled. Use --enable-chromadb=true and ensure ChromaDB is running")
	}

	fmt.Println("\n📋 Step 1: Generating execution plan from user instructions...")

	// Generate step plan from LLM
	steps, err := generateStepPlan(ctx, openaiClient, userInstructions, tools)
	if err != nil {
		return "", fmt.Errorf("failed to generate step plan: %v", err)
	}

	fmt.Printf("\n✅ Generated %d steps:\n", len(steps))
	for i, step := range steps {
		fmt.Printf("  %d. [%s] %s\n", i+1, step.ToolName, step.Description)
	}

	fmt.Println("\n🚀 Step 2: Executing plan with ChromaDB-driven element selection...")

	// Execute steps
	var executionLog strings.Builder
	executionLog.WriteString("ChromaDB-Driven Workflow Execution:\n\n")

	for i, step := range steps {
		fmt.Printf("\n▶️  Executing Step %d/%d: [%s] %s\n", i+1, len(steps), step.ToolName, step.Description)

		result, err := executeStep(ctx, session, step)
		if err != nil {
			errMsg := fmt.Sprintf("❌ Step %d failed: %v\n", i+1, err)
			fmt.Print(errMsg)
			executionLog.WriteString(errMsg)
			return executionLog.String(), err
		}

		successMsg := fmt.Sprintf("✅ Step %d completed\n", i+1)
		fmt.Print(successMsg)
		executionLog.WriteString(fmt.Sprintf("Step %d: [%s] %s\n", i+1, step.ToolName, step.Description))
		executionLog.WriteString(fmt.Sprintf("Result: %s\n\n", truncateString(result, 200)))

		// Add delay between steps to avoid overwhelming the system
		if i < len(steps)-1 {
			time.Sleep(2 * time.Second)
		}
	}

	fmt.Println("\n🎉 Workflow completed successfully!")
	executionLog.WriteString("\n🎉 All steps executed successfully\n")

	return executionLog.String(), nil
}

// generateStepPlan calls LLM to convert user instructions into structured steps
func generateStepPlan(ctx context.Context, openaiClient *openai.Client, userInstructions string, tools []*mcp.Tool) ([]Step, error) {
	// Build tool descriptions for the prompt
	var toolDescriptions strings.Builder
	toolDescriptions.WriteString("Available tools:\n")
	for _, tool := range tools {
		toolDescriptions.WriteString(fmt.Sprintf("- %s: %s\n", tool.Name, tool.Description))
	}

	// Create system prompt for step generation
	systemPrompt := `You are a browser automation planner. Your job is to convert user instructions into a structured step-by-step plan.

CRITICAL FORMATTING RULES:
1. Output ONE step per line
2. Each step MUST start with [TOOL:toolname] where toolname is one of the available tools
3. After the tool prefix, write a natural language description of what to do
4. For multi-parameter tools like type_text, include ALL details in the description

` + toolDescriptions.String() + `

STEP FORMAT EXAMPLES:
[TOOL:navigate] Navigate to https://www.google.com
[TOOL:aria_snapshot_v2] Take snapshot of the page to find elements
[TOOL:click_button] Click the search button
[TOOL:type_text] Type "artificial intelligence" into the search box
[TOOL:click_link] Click the link for Wikipedia
[TOOL:screenshot] Take a screenshot of the results

IMPORTANT GUIDELINES:
- If user instructions are brief, elaborate them into detailed steps
- If user instructions are already detailed, you can use them as-is or refine if needed
- ALWAYS start with a navigate step if a URL is implied or mentioned
- After navigate, ALWAYS include aria_snapshot_v2 to populate the page structure
- Be specific about what text to type, which buttons/links to click
- Use aria_snapshot_v2 after navigation to enable element finding
- End with screenshot or workflow_complete when appropriate

CRITICAL - AUTHENTICATION & LOGIN HANDLING:
- NEVER include login, sign-in, authentication, password, or credential-related steps
- DO NOT generate steps for clicking login buttons, typing usernames/passwords, or any authentication flow
- If user mentions "login" or "sign in", SKIP those steps entirely in your plan
- Start the plan AFTER login is assumed to be complete
- The system will automatically pause after navigation to allow the user to login manually
- Example: If user says "Login to GitHub and search for repos", generate steps starting with the search, NOT the login
- Assume the user is already logged in when generating post-navigation steps

Now convert the following user instructions into steps:`

	messages := []openai.ChatCompletionMessage{
		{
			Role:    openai.ChatMessageRoleSystem,
			Content: systemPrompt,
		},
		{
			Role:    openai.ChatMessageRoleUser,
			Content: userInstructions,
		},
	}

	req := openai.ChatCompletionRequest{
		Model:       openai.GPT4o,
		Messages:    messages,
		Temperature: 0.3,
	}

	resp, err := openaiClient.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %v", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	planText := resp.Choices[0].Message.Content
	fmt.Printf("\n📝 LLM Generated Plan:\n%s\n", planText)

	// Parse the plan into steps
	steps := parseSteps(planText)
	if len(steps) == 0 {
		return nil, fmt.Errorf("failed to parse any steps from LLM response")
	}

	return steps, nil
}

// parseSteps parses LLM output into Step structs
func parseSteps(planText string) []Step {
	var steps []Step

	// Match lines starting with [TOOL:toolname]
	stepRegex := regexp.MustCompile(`(?m)^\[TOOL:(\w+)\]\s*(.+)$`)
	matches := stepRegex.FindAllStringSubmatch(planText, -1)

	for _, match := range matches {
		if len(match) == 3 {
			steps = append(steps, Step{
				ToolName:     match[1],
				Description:  strings.TrimSpace(match[2]),
				OriginalLine: match[0],
			})
		}
	}

	return steps
}

// executeStep executes a single step using ChromaDB for element selection
func executeStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	switch step.ToolName {
	case "navigate":
		return executeNavigateStep(ctx, session, step)
	case "aria_snapshot", "aria_snapshot_v2":
		return executeAriaSnapshotStep(ctx, session, step)
	case "screenshot", "refresh_page", "close_browser", "workflow_complete":
		// These tools don't need element selection
		return executeSimpleTool(ctx, session, step.ToolName)
	case "click_button", "click_link", "click":
		return executeClickStep(ctx, session, step)
	case "type_text":
		return executeTypeTextStep(ctx, session, step)
	case "select_dropdown", "choose_option":
		return executeSelectStep(ctx, session, step)
	default:
		return "", fmt.Errorf("unsupported tool: %s", step.ToolName)
	}
}

// executeNavigateStep handles navigation and auto-snapshots for ChromaDB population
func executeNavigateStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	// Extract URL from description
	urlRegex := regexp.MustCompile(`https?://[^\s]+`)
	url := urlRegex.FindString(step.Description)
	if url == "" {
		// Try to extract domain and construct URL
		if strings.Contains(strings.ToLower(step.Description), "google") {
			url = "https://www.google.com"
		} else {
			return "", fmt.Errorf("could not extract URL from: %s", step.Description)
		}
	}

	fmt.Printf("  🌐 Navigating to: %s\n", url)

	args := map[string]interface{}{"url": url}
	argsJSON, _ := json.Marshal(args)

	result, err := executeMCPTool(ctx, session, "navigate", string(argsJSON))
	if err != nil {
		return "", err
	}

	// Auto-snapshot after navigation to populate ChromaDB
	fmt.Println("  📸 Auto-taking ARIA snapshot (V2) to populate ChromaDB...")
	time.Sleep(2 * time.Second) // Wait for page load

	snapshotResult, err := executeMCPTool(ctx, session, "aria_snapshot_v2", "{}")
	if err != nil {
		log.Printf("WARNING: Auto-snapshot failed: %v", err)
	} else {
		fmt.Printf("  ✅ ChromaDB populated with %d elements\n", totalElementsStored)
	}

	// Always pause for manual login after navigation
	fmt.Println("\n" + strings.Repeat("━", 70))
	fmt.Println("🔐 LOGIN CHECKPOINT")
	fmt.Println(strings.Repeat("━", 70))
	fmt.Println("If login/authentication is required, please complete it now.")
	fmt.Println("Otherwise, just press ENTER to continue with automation...")
	fmt.Println(strings.Repeat("━", 70))
	fmt.Print("→ Press ENTER when ready: ")

	// Wait for user confirmation
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')

	fmt.Println("✅ Continuing with automation...\n")

	return result + "\n[Auto-snapshot: " + snapshotResult + "]", nil
}

// executeAriaSnapshotStep executes aria_snapshot_v2 tool
func executeAriaSnapshotStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	fmt.Println("  📸 Taking ARIA snapshot (V2)...")
	return executeMCPTool(ctx, session, "aria_snapshot_v2", "{}")
}

// executeSimpleTool executes tools that don't require parameters
func executeSimpleTool(ctx context.Context, session *mcp.ClientSession, toolName string) (string, error) {
	fmt.Printf("  🔧 Executing %s...\n", toolName)
	return executeMCPTool(ctx, session, toolName, "{}")
}

// executeClickStep executes click actions using ChromaDB to find element
func executeClickStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	// Query ChromaDB for the element
	selector, err := queryChromaDBForElement(ctx, step.Description)
	if err != nil {
		return "", fmt.Errorf("failed to find element: %v", err)
	}

	fmt.Printf("  🖱️  Clicking element: %s\n", selector)

	args := map[string]interface{}{"selector": selector}
	argsJSON, _ := json.Marshal(args)

	return executeMCPTool(ctx, session, step.ToolName, string(argsJSON))
}

// executeTypeTextStep executes type_text using ChromaDB to find element
func executeTypeTextStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	// Parse text to type and target element from description
	// Example: "Type 'artificial intelligence' into the search box"
	textRegex := regexp.MustCompile(`['"]([^'"]+)['"]`)
	textMatch := textRegex.FindStringSubmatch(step.Description)
	if textMatch == nil {
		return "", fmt.Errorf("could not extract text to type from: %s", step.Description)
	}
	textToType := textMatch[1]

	// Extract target description (everything after "into" or "in")
	targetDesc := step.Description
	if idx := strings.Index(strings.ToLower(step.Description), " into "); idx != -1 {
		targetDesc = step.Description[idx+6:]
	} else if idx := strings.Index(strings.ToLower(step.Description), " in "); idx != -1 {
		targetDesc = step.Description[idx+4:]
	}

	// Query ChromaDB for the element
	selector, err := queryChromaDBForElement(ctx, targetDesc)
	if err != nil {
		return "", fmt.Errorf("failed to find element for typing: %v", err)
	}

	fmt.Printf("  ⌨️  Typing '%s' into element: %s\n", textToType, selector)

	args := map[string]interface{}{
		"selector": selector,
		"text":     textToType,
	}
	argsJSON, _ := json.Marshal(args)

	return executeMCPTool(ctx, session, "type_text", string(argsJSON))
}

// executeSelectStep executes select/dropdown actions using ChromaDB
func executeSelectStep(ctx context.Context, session *mcp.ClientSession, step Step) (string, error) {
	// Query ChromaDB for the element
	selector, err := queryChromaDBForElement(ctx, step.Description)
	if err != nil {
		return "", fmt.Errorf("failed to find element: %v", err)
	}

	fmt.Printf("  📋 Selecting from element: %s\n", selector)

	args := map[string]interface{}{"selector": selector}
	argsJSON, _ := json.Marshal(args)

	return executeMCPTool(ctx, session, step.ToolName, string(argsJSON))
}

// queryChromaDBForElement queries ChromaDB for the most relevant element
func queryChromaDBForElement(ctx context.Context, queryText string) (string, error) {
	if globalChromaCollection == nil {
		return "", fmt.Errorf("ChromaDB collection not initialized")
	}

	fmt.Printf("  🔍 Querying ChromaDB for: '%s'\n", queryText)

	// Query ChromaDB for semantically similar elements using the client wrapper
	// We fetch top 5 results to allow filtering out structural nodes (like RootWebArea)
	results, err := globalChromaClient.QueryDocuments(globalChromaCollection, []string{queryText}, 5)
	if err != nil {
		return "", fmt.Errorf("ChromaDB query failed: %v", err)
	}

	if results == nil {
		return "", fmt.Errorf("no results returned from ChromaDB")
	}

	// Get groups
	idGroups := results.GetIDGroups()
	metadataGroups := results.GetMetadatasGroups()
	documentGroups := results.GetDocumentsGroups()

	if len(idGroups) == 0 || len(idGroups[0]) == 0 {
		return "", fmt.Errorf("no matching elements found in ChromaDB for: %s", queryText)
	}

	var bestSelector string
	var bestDisplayText string
	var bestElementType string

	// Iterate through results to find the best non-structural element
	for i := 0; i < len(idGroups[0]); i++ {
		metadata := metadataGroups[0][i]
		displayText := documentGroups[0][i].ContentString()
		elementType, _ := metadata.GetString("element_type")

		// Skip structural roles that are almost never the intended target for clicks/typing
		// These often match page titles or large containers that shouldn't be clicked.
		if elementType == "RootWebArea" || elementType == "WebArea" || elementType == "document" || elementType == "none" {
			continue
		}

		// Extract selector
		var selector string
		if primarySelector, ok := metadata.GetString("primary_selector"); ok && primarySelector != "" {
			selector = primarySelector
		} else if altSelector, ok := metadata.GetString("alt_selector"); ok && altSelector != "" {
			selector = altSelector
		} else if displayText != "" {
			selector = sanitizeDisplayTextForSelector(displayText)
		}

		if selector != "" {
			bestSelector = selector
			bestDisplayText = displayText
			bestElementType = elementType
			break
		}
	}

	// Fallback to the very first result if we couldn't find a "good" one
	if bestSelector == "" {
		metadata := metadataGroups[0][0]
		bestDisplayText = documentGroups[0][0].ContentString()
		bestElementType, _ = metadata.GetString("element_type")

		if primarySelector, ok := metadata.GetString("primary_selector"); ok && primarySelector != "" {
			bestSelector = primarySelector
		} else if altSelector, ok := metadata.GetString("alt_selector"); ok && altSelector != "" {
			bestSelector = altSelector
		} else {
			bestSelector = sanitizeDisplayTextForSelector(bestDisplayText)
		}
	}

	// Log if we are using a fallback selector
	if !strings.Contains(bestSelector, "[") && !strings.Contains(bestSelector, "#") && !strings.Contains(bestSelector, ".") {
		fmt.Printf("  ⚠️  No robust selector found, using display text as fallback\n")
		fmt.Printf("      Original text: '%s'\n", truncateString(bestDisplayText, 60))
		fmt.Printf("      Sanitized selector: '%s'\n", truncateString(bestSelector, 60))
	}

	fmt.Printf("  ✓ Found: [%s] '%s' → %s\n", bestElementType, truncateString(bestDisplayText, 40), truncateString(bestSelector, 60))

	return bestSelector, nil
}

// sanitizeDisplayTextForSelector cleans up display text for use as a selector
func sanitizeDisplayTextForSelector(text string) string {
	// Remove common ARIA/selector syntax that might confuse the browser
	// Remove things like [aria-label="..."], selectors, etc.

	// If text contains newlines, take only the first line
	if idx := strings.Index(text, "\n"); idx != -1 {
		text = text[:idx]
	}

	// If text contains [ or other selector-like syntax, just extract the clean text
	// Look for pattern like: text [aria-label="something"]
	if idx := strings.Index(text, "["); idx != -1 {
		// Take everything before the bracket
		text = strings.TrimSpace(text[:idx])
	}

	// Trim any extra whitespace
	text = strings.TrimSpace(text)

	return text
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
