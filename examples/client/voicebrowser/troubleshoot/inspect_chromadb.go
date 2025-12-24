// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// inspect_chromadb exports all collections and their documents from ChromaDB to a JSON file
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/examples/client/voicebrowser/chromadb"
)

type ElementInfo struct {
	ID             string                 `json:"id"`
	Document       string                 `json:"document"`
	Metadata       map[string]interface{} `json:"metadata"`
	CollectionName string                 `json:"collection_name"`
}

func main() {
	var outputPath string
	flag.StringVar(&outputPath, "output", "/tmp/aria_structure.json", "Path to store the JSON output")
	flag.Parse()

	ctx := context.Background()

	// Connect to ChromaDB
	client, err := chromadb.NewClient(ctx, "http://localhost:8000")
	if err != nil {
		log.Fatalf("Failed to connect to ChromaDB: %v", err)
	}

	fmt.Println("🔍 Listing all ChromaDB collections...")

	// List all collections
	collections, err := client.ListCollections()
	if err != nil {
		log.Fatalf("Failed to list collections: %v", err)
	}

	if len(collections) == 0 {
		fmt.Println("✓ No collections found!")
		return
	}

	fmt.Printf("Found %d collection(s). Extracting elements...\n", len(collections))

	var allElements []ElementInfo

	for _, coll := range collections {
		fmt.Printf("  - Processing collection: %s\n", coll.Name())

		// Get all documents from the collection
		result, err := client.GetDocuments(coll, nil)
		if err != nil {
			log.Printf("❌ Failed to get documents from collection '%s': %v", coll.Name(), err)
			continue
		}

		ids := result.GetIDs()
		docs := result.GetDocuments()
		metadatas := result.GetMetadatas()

		for i := 0; i < len(ids); i++ {
			elem := ElementInfo{
				ID:             string(ids[i]),
				Document:       docs[i].ContentString(),
				Metadata:       make(map[string]interface{}),
				CollectionName: coll.Name(),
			}

			// Extract metadata fields
			// We know the fields we use in voicebrowser
			fields := []string{"element_type", "aria_label", "primary_selector", "alt_selector", "url", "timestamp", "session_id"}
			for _, field := range fields {
				if val, ok := metadatas[i].GetString(field); ok {
					elem.Metadata[field] = val
				}
			}

			allElements = append(allElements, elem)
		}
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(allElements, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal elements to JSON: %v", err)
	}

	// Write to file
	err = os.WriteFile(outputPath, jsonData, 0644)
	if err != nil {
		log.Fatalf("Failed to write JSON to %s: %v", outputPath, err)
	}

	fmt.Printf("\n✅ Successfully exported %d elements to %s\n", len(allElements), outputPath)
}
