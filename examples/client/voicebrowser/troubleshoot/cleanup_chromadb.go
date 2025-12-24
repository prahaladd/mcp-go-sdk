// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// cleanup_chromadb removes all collections from ChromaDB
package main

import (
	"context"
	"fmt"
	"log"
	
	"github.com/modelcontextprotocol/go-sdk/examples/client/voicebrowser/chromadb"
)

func main() {
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
		fmt.Println("✓ No collections found - ChromaDB is already clean!")
		return
	}
	
	fmt.Printf("Found %d collection(s):\n", len(collections))
	for _, coll := range collections {
		fmt.Printf("  - %s\n", coll.Name())
	}
	
	// Delete all collections
	fmt.Println("\n🗑️  Deleting all collections...")
	deletedCount := 0
	for _, coll := range collections {
		name := coll.Name()
		err := client.DeleteCollection(name)
		if err != nil {
			log.Printf("❌ Failed to delete collection '%s': %v", name, err)
		} else {
			fmt.Printf("  ✓ Deleted: %s\n", name)
			deletedCount++
		}
	}
	
	fmt.Printf("\n✅ ChromaDB cleanup complete! Deleted %d collection(s).\n", deletedCount)
}
