package chromadb

import (
	"context"
	"fmt"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
)

// Client encapsulates ChromaDB operations using v2 API.
//
// The Client provides a simplified interface to ChromaDB's vector database operations,
// handling context management and providing consistent error handling across all operations.
// It wraps the official chroma-go v2 API client.
type Client struct {
	client chroma.Client   // The underlying ChromaDB HTTP client
	ctx    context.Context // Context used for all operations
}

// NewClient creates a new ChromaDB client wrapper using v2 API.
//
// ChromaDB API Details:
// - The v2 API uses HTTP/REST endpoints for all operations
// - Default endpoint: http://localhost:8000
// - The client maintains a connection pool for efficient request handling
// - All operations are stateless and can be retried safely
//
// Parameters:
//   - ctx: Context for all subsequent operations (cancellation, timeouts, etc.)
//   - basePath: Base URL for ChromaDB server. Empty string uses default "http://localhost:8000"
//
// Returns:
//   - *Client: Configured client wrapper ready for operations
//   - error: Connection or configuration errors
//
// Example:
//
//	client, err := NewClient(context.Background(), "http://localhost:8000")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, basePath string) (*Client, error) {
	var client chroma.Client
	var err error

	if basePath == "" {
		// Default to localhost:8000
		client, err = chroma.NewHTTPClient()
	} else {
		client, err = chroma.NewHTTPClient(chroma.WithBaseURL(basePath))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create chroma client: %w", err)
	}

	return &Client{
		client: client,
		ctx:    ctx,
	}, nil
}

// CreateCollection creates a new collection in ChromaDB.
//
// ChromaDB API Details:
// - Endpoint: POST /api/v2/collections
// - Collections are the primary organizational unit in ChromaDB
// - Each collection has:
//   - A unique name (used as identifier)
//   - An embedding function (transforms text to vectors)
//   - Metadata (optional key-value pairs)
//   - Distance metric (L2, cosine, or inner product - defaults to L2)
//   - Configuration settings (HNSW index parameters, etc.)
//
// - Collections store embeddings (vectors) along with:
//   - Documents (the original text)
//   - Metadata (structured data for filtering)
//   - IDs (unique identifiers for each document)
//
// Embedding Functions:
// - Transform text documents into numerical vectors (embeddings)
// - Must be consistent: same text → same embedding
// - Common options:
//   - ConsistentHashEmbeddingFunction: Simple deterministic hashing (for testing)
//   - OpenAI embeddings: High-quality semantic embeddings
//   - Sentence transformers: Local models like all-MiniLM-L6-v2
//   - Custom functions: Implement the EmbeddingFunction interface
//
// Parameters:
//   - name: Unique collection name (lowercase recommended, no spaces)
//   - embeddingFunction: Function to convert text to vectors (nil uses server default)
//
// Returns:
//   - chroma.Collection: Handle to the created collection for subsequent operations
//   - error: If collection already exists or creation fails
//
// Example:
//
//	ef := embeddings.NewConsistentHashEmbeddingFunction()
//	collection, err := client.CreateCollection("my-docs", ef)
func (c *Client) CreateCollection(name string, embeddingFunction embeddings.EmbeddingFunction) (chroma.Collection, error) {
	var opts []chroma.CreateCollectionOption

	if embeddingFunction != nil {
		opts = append(opts, chroma.WithEmbeddingFunctionCreate(embeddingFunction))
	}

	collection, err := c.client.CreateCollection(c.ctx, name, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection %s: %w", name, err)
	}
	return collection, nil
}

// GetCollection retrieves an existing collection by name.
//
// ChromaDB API Details:
// - Endpoint: GET /api/v2/collections/{name}
// - Returns collection metadata and configuration
// - Does NOT load all documents into memory (collections are lazy)
// - The collection handle is used for all document operations
//
// Important Notes:
// - The embedding function must match the one used during creation
// - If you don't know the original embedding function, you can:
//  1. Use nil (server will use its default)
//  2. Check collection metadata for embedding function info
//  3. Use the same function consistently across your application
//
// - Collection handles are lightweight and can be obtained multiple times
// - Each collection has an internal cache that's updated on operations
//
// Parameters:
//   - name: Collection name to retrieve
//   - embeddingFunction: Must match the function used during creation
//
// Returns:
//   - chroma.Collection: Handle to the existing collection
//   - error: If collection doesn't exist or retrieval fails
//
// Example:
//
//	ef := embeddings.NewConsistentHashEmbeddingFunction()
//	collection, err := client.GetCollection("my-docs", ef)
func (c *Client) GetCollection(name string, embeddingFunction embeddings.EmbeddingFunction) (chroma.Collection, error) {
	var opts []chroma.GetCollectionOption

	if embeddingFunction != nil {
		opts = append(opts, chroma.WithEmbeddingFunctionGet(embeddingFunction))
	}

	collection, err := c.client.GetCollection(c.ctx, name, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection %s: %w", name, err)
	}
	return collection, nil
}

// ListCollections retrieves all collections in the current database.
//
// ChromaDB API Details:
// - Endpoint: GET /api/v2/collections
// - Returns metadata for all collections in the current tenant/database
// - Does NOT load document data (only collection metadata)
//
// Multi-tenancy:
// - ChromaDB supports tenants (isolated namespaces)
// - Each tenant can have multiple databases
// - Default tenant: "default_tenant"
// - Default database: "default_database"
// - This method lists collections in the current database context
//
// Collection Metadata Includes:
// - Name: Unique identifier
// - ID: Internal UUID
// - Metadata: Custom key-value pairs
// - Dimension: Vector dimension (set after first document add)
// - Configuration: HNSW parameters, distance function, etc.
//
// Returns:
//   - []chroma.Collection: Slice of collection handles
//   - error: If listing fails (connection issues, auth, etc.)
//
// Example:
//
//	collections, err := client.ListCollections()
//	for _, coll := range collections {
//	    fmt.Printf("Collection: %s (ID: %s)\n", coll.Name(), coll.ID())
//	}
func (c *Client) ListCollections() ([]chroma.Collection, error) {
	collections, err := c.client.ListCollections(c.ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list collections: %w", err)
	}
	return collections, nil
}

// DeleteCollection permanently removes a collection and all its data.
//
// ChromaDB API Details:
// - Endpoint: DELETE /api/v2/collections/{name}
// - Deletes ALL documents, embeddings, and metadata in the collection
// - This operation is IRREVERSIBLE - no undo or recovery
// - Frees up storage space and index memory
//
// What Gets Deleted:
// - All document embeddings (vectors)
// - All document texts
// - All document metadata
// - HNSW index structures
// - Collection configuration and metadata
//
// Important Considerations:
// - Be cautious with this operation in production
// - Consider backing up important data before deletion
// - Any active collection handles become invalid after deletion
// - Attempting to use a deleted collection will result in errors
//
// Performance:
// - Deletion is typically fast (< 1 second for most collections)
// - Large collections (millions of documents) may take longer
// - The operation is atomic - either fully succeeds or fails
//
// Parameters:
//   - name: Name of the collection to delete
//
// Returns:
//   - error: If collection doesn't exist or deletion fails
//
// Example:
//
//	err := client.DeleteCollection("old-collection")
//	if err != nil {
//	    log.Printf("Failed to delete: %v", err)
//	}
func (c *Client) DeleteCollection(name string) error {
	err := c.client.DeleteCollection(c.ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete collection %s: %w", name, err)
	}
	return nil
}

// AddDocuments adds new documents to a collection with their embeddings.
//
// ChromaDB API Details:
// - Endpoint: POST /api/v2/collections/{id}/add
// - Generates embeddings using the collection's embedding function
// - Stores documents, embeddings, metadata, and IDs together
// - Each document is a single unit with multiple associated data points
//
// Document Structure:
// Each document consists of:
// 1. ID: Unique identifier (string)
//   - Must be unique within the collection
//   - Can be provided or auto-generated (using ULID generator)
//   - Used for updates, deletes, and retrieval
//
// 2. Document Text: The actual content (string)
//   - Can be a sentence, paragraph, or entire document
//   - This text is embedded into a vector using the embedding function
//   - Original text is stored for retrieval
//
// 3. Embedding: Numerical vector representation ([]float32)
//   - Auto-generated from document text via embedding function
//   - Dimension determined by embedding function (e.g., 384 for MiniLM, 1536 for OpenAI)
//   - Used for similarity search
//
// 4. Metadata: Structured key-value pairs (map[string]interface{})
//   - Can store any JSON-serializable data
//   - Used for filtering queries
//   - Common uses: source, category, date, author, etc.
//   - Supports nested structures
//
// Batching Behavior:
// - ChromaDB processes documents in batches for efficiency
// - Default batch size varies by deployment (typically 100-1000)
// - Large document sets are automatically batched
// - All documents in a batch succeed or fail together (atomic operation)
//
// ID Generation:
// - If IDs provided: Uses your IDs (must be unique)
// - If IDs not provided: Auto-generates using ULID (Universally Unique Lexicographically Sortable ID)
//   - ULIDs are time-sortable
//   - Format: 26 characters (base32 encoded)
//   - Example: "01ARZ3NDEKTSV4RRFFQ69G5FAV"
//
// Embedding Process:
// 1. Document text → Embedding function → Vector
// 2. Vector stored in HNSW (Hierarchical Navigable Small World) index
// 3. HNSW enables fast approximate nearest neighbor search
// 4. Index is automatically updated with each add operation
//
// Error Handling:
// - Duplicate IDs: Returns error, no documents added
// - Empty documents: Returns error
// - Dimension mismatch: If first document, sets dimension; otherwise must match
// - Length mismatch: IDs/metadatas length != documents length causes validation error
//
// Parameters:
//   - collection: Target collection handle
//   - documents: Array of document texts to add
//   - metadatas: Array of metadata objects (MUST exactly match documents length if provided)
//   - ids: Array of unique IDs (MUST exactly match documents length if provided, empty array triggers auto-generation)
//
// CRITICAL: If you provide IDs, the length MUST exactly match the documents length.
// The ChromaDB API validation will reject mismatched lengths with an error like:
// "documents (X) must match the number of ids (Y)"
//
// Returns:
//   - error: If addition fails (duplicate IDs, length mismatch, validation errors, etc.)
//
// Example:
//
//	documents := []string{"First doc", "Second doc"}
//	metadatas := []chroma.DocumentMetadata{
//	    chroma.NewMetadata(chroma.NewStringAttribute("source", "web")),
//	    chroma.NewMetadata(chroma.NewStringAttribute("source", "api")),
//	}
//	ids := []string{"doc1", "doc2"}
//	err := client.AddDocuments(collection, documents, metadatas, ids)
func (c *Client) AddDocuments(collection chroma.Collection, documents []string, metadatas []chroma.DocumentMetadata, ids []string) error {
	opts := []chroma.CollectionAddOption{}

	if len(ids) > 0 {
		opts = append(opts, chroma.WithIDs(stringsToDocumentIDs(ids)...))
	} else {
		opts = append(opts, chroma.WithIDGenerator(chroma.NewULIDGenerator()))
	}

	if len(documents) > 0 {
		opts = append(opts, chroma.WithTexts(documents...))
	}

	if len(metadatas) > 0 {
		opts = append(opts, chroma.WithMetadatas(metadatas...))
	}

	err := collection.Add(c.ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to add documents: %w", err)
	}
	return nil
}

// GetDocuments retrieves specific documents from a collection by their IDs.
//
// ChromaDB API Details:
// - Endpoint: POST /api/v2/collections/{id}/get
// - Fetches documents by exact ID match
// - Returns documents with all associated data (text, embeddings, metadata)
// - Efficient for known IDs (no vector search required)
//
// Retrieval Mechanism:
// - Uses direct lookup (O(1) complexity per ID)
// - Much faster than similarity search for known IDs
// - Returns results in arbitrary order (not necessarily input order)
// - Missing IDs are silently skipped (no error for non-existent IDs)
//
// What Gets Retrieved:
// By default, returns:
// - Document IDs
// - Document texts (if stored)
// - Metadata (if exists)
// - Embeddings (can be excluded for efficiency)
//
// Filtering Options (via CollectionGetOption):
// - WithIDsGet: Specify exact IDs to retrieve
// - WithWhereGet: Filter by metadata conditions
//   - Supports: $eq, $ne, $gt, $gte, $lt, $lte, $in, $nin
//   - Example: {"category": {"$eq": "news"}}
//
// - WithWhereDocumentGet: Filter by document content
//   - Supports: $contains, $not_contains
//   - Example: {"$contains": "machine learning"}
//
// - WithIncludeGet: Control what data to return
//   - Options: "documents", "metadatas", "embeddings", "distances"
//   - Reduces payload size if you don't need all data
//
// - WithLimitGet: Maximum number of results
// - WithOffsetGet: Pagination support
//
// Result Structure:
// - GetResult interface provides access to:
//   - GetIDs(): []string - Document IDs
//   - GetDocuments(): []Document - Document content objects
//   - GetMetadatas(): []DocumentMetadata - Metadata objects
//   - GetEmbeddings(): []Embedding - Vector embeddings (if included)
//
// Use Cases:
// - Retrieving specific documents after a search
// - Fetching documents for display/editing
// - Validating document existence
// - Bulk export of known documents
// - Checking document metadata
//
// Performance:
// - Very fast for small ID sets (< 100 IDs)
// - Scales linearly with number of IDs
// - No embedding computation required
// - Network latency is primary bottleneck
//
// Parameters:
//   - collection: Source collection handle
//   - ids: Array of document IDs to retrieve (empty returns all, use with caution)
//
// Returns:
//   - chroma.GetResult: Result object containing matched documents and data
//   - error: If retrieval fails (connection issues, invalid collection, etc.)
//
// Example:
//
//	result, err := client.GetDocuments(collection, []string{"doc1", "doc2"})
//	if err == nil {
//	    for i, id := range result.GetIDs() {
//	        fmt.Printf("ID: %s, Text: %s\n", id, result.GetDocuments()[i].ContentString())
//	    }
//	}
func (c *Client) GetDocuments(collection chroma.Collection, ids []string) (chroma.GetResult, error) {
	opts := []chroma.CollectionGetOption{}

	if len(ids) > 0 {
		opts = append(opts, chroma.WithIDsGet(stringsToDocumentIDs(ids)...))
	}

	result, err := collection.Get(c.ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to get documents: %w", err)
	}
	return result, nil
}

// QueryDocuments performs semantic similarity search on a collection.
//
// ChromaDB API Details:
// - Endpoint: POST /api/v2/collections/{id}/query
// - Uses Approximate Nearest Neighbor (ANN) search via HNSW index
// - Finds documents with embeddings most similar to query embeddings
// - Returns results ranked by similarity (distance metric)
//
// How Semantic Search Works:
// 1. Query Text → Embedding Function → Query Vector
// 2. HNSW Index Search: Find nearest neighbors in vector space
// 3. Calculate distances between query vector and candidate vectors
// 4. Rank and return top N results
//
// Distance Metrics (set at collection creation):
// - L2 (Euclidean Distance): √(Σ(a_i - b_i)²)
//   - Measures straight-line distance in vector space
//   - Range: [0, ∞), lower is more similar
//   - Default metric, good for most use cases
//
// - Cosine Distance: 1 - (a·b)/(||a||×||b||)
//   - Measures angle between vectors
//   - Range: [0, 2], lower is more similar
//   - Best for text embeddings (ignores magnitude)
//
// - Inner Product: -(a·b)
//   - Dot product of vectors (negated for consistency)
//   - Range: (-∞, ∞), lower is more similar
//   - Useful for normalized embeddings
//
// HNSW Index Algorithm:
// - Hierarchical Navigable Small World graph structure
// - Provides logarithmic search time: O(log N)
// - Trade-off between accuracy and speed via parameters:
//   - ef_construction: Build-time quality (higher = better but slower)
//   - ef_search: Query-time quality (higher = more accurate but slower)
//   - M: Number of connections per node (higher = better recall)
//
// Query Processing:
// 1. Query text is embedded using collection's embedding function
// 2. HNSW index is traversed to find approximate nearest neighbors
// 3. Candidate documents are scored using distance metric
// 4. Top N results are returned with their distances
//
// Query Options (via CollectionQueryOption):
// - WithQueryTexts: Text queries to search for
//   - Multiple queries supported (parallel search)
//   - Each query returns its own result set
//
// - WithNResults: Number of top results per query
//   - Default: 10
//   - Range: 1 to collection size
//   - Higher values = more results but slower
//
// - WithWhereQuery: Metadata filtering
//   - Pre-filter candidates before vector search
//   - Operators: $eq, $ne, $gt, $gte, $lt, $lte, $in, $nin, $and, $or
//   - Example: {"category": {"$eq": "news"}, "year": {"$gte": 2020}}
//   - Filters reduce search space, improving performance
//
// - WithWhereDocumentQuery: Document content filtering
//   - Filter by text content patterns
//   - Operators: $contains, $not_contains
//   - Example: {"$contains": "machine learning"}
//   - Applied after embedding search
//
// - WithIncludeQuery: Control returned data
//   - Options: "documents", "metadatas", "embeddings", "distances"
//   - Default: all except embeddings
//   - Exclude unnecessary data to reduce payload size
//
// - WithQueryEmbeddings: Use pre-computed query vectors
//   - Bypass text→embedding step
//   - Useful when you have vectors from another source
//   - Must match collection's embedding dimension
//
// Result Structure:
// - QueryResult provides grouped results:
//   - GetDocumentsGroups(): [][]Document - Documents per query
//   - GetIDGroups(): [][]string - IDs per query
//   - GetMetadatasGroups(): [][]DocumentMetadata - Metadata per query
//   - GetDistancesGroups(): [][]float32 - Distances per query
//   - GetEmbeddingsGroups(): [][]Embedding - Embeddings per query (if included)
//
// - Structure: [query_index][result_index]
//   - First dimension: One per query text
//   - Second dimension: Top N results for that query
//   - All arrays parallel (same indices correspond to same document)
//
// Performance Characteristics:
// - Query time: O(log N) where N = collection size
// - Typical latency: 1-100ms depending on collection size
// - Factors affecting speed:
//   - Collection size (larger = slower)
//   - nResults count (more = slower)
//   - Metadata filters (complex filters = slower)
//   - HNSW parameters (higher ef = slower but more accurate)
//
// Accuracy vs Speed:
// - HNSW is approximate, not exact
// - May miss true nearest neighbors (especially in large collections)
// - Tune ef_search for accuracy/speed trade-off
// - Typical accuracy: 95-99% recall of true neighbors
//
// Use Cases:
// - Semantic document search ("find similar documents")
// - Question answering ("find passages relevant to question")
// - Recommendation systems ("find similar items")
// - Duplicate detection ("find near-duplicate documents")
// - Clustering and classification
//
// Best Practices:
// - Use metadata filters to narrow search space when possible
// - Request only needed data fields (WithIncludeQuery)
// - Cache frequently used queries
// - Batch multiple queries together
// - Tune nResults based on your needs (avoid over-fetching)
//
// Parameters:
//   - collection: Collection to search in
//   - queryTexts: Array of text queries (each returns independent results)
//   - nResults: Number of top results to return per query
//
// Returns:
//   - chroma.QueryResult: Grouped results with documents, distances, and metadata
//   - error: If query fails (invalid parameters, connection issues, etc.)
//
// Example:
//
//	results, err := client.QueryDocuments(
//	    collection,
//	    []string{"machine learning algorithms", "neural networks"},
//	    5,
//	)
//	if err == nil {
//	    // Process first query's results
//	    for i, doc := range results.GetDocumentsGroups()[0] {
//	        distance := results.GetDistancesGroups()[0][i]
//	        fmt.Printf("Result %d: %s (distance: %.4f)\n", i+1, doc.ContentString(), distance)
//	    }
//	}
func (c *Client) QueryDocuments(collection chroma.Collection, queryTexts []string, nResults int) (chroma.QueryResult, error) {
	opts := []chroma.CollectionQueryOption{
		chroma.WithQueryTexts(queryTexts...),
	}

	if nResults > 0 {
		opts = append(opts, chroma.WithNResults(nResults))
	}

	result, err := collection.Query(c.ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to query documents: %w", err)
	}
	return result, nil
}

// DeleteDocuments removes specific documents from a collection.
//
// ChromaDB API Details:
// - Endpoint: POST /api/v2/collections/{id}/delete
// - Removes documents and their associated data (embeddings, metadata)
// - Updates the HNSW index (removes vectors from search space)
// - Operation is atomic per batch
//
// What Gets Deleted:
// For each matching document:
// - Document ID (freed for reuse)
// - Document text (if stored)
// - Embedding vector (removed from HNSW index)
// - All associated metadata
// - Index connections (HNSW graph edges)
//
// Deletion Methods:
// 1. By IDs (WithIDsDelete):
//   - Delete specific documents by exact ID match
//   - Most common and efficient method
//   - Example: []string{"doc1", "doc2", "doc3"}
//
// 2. By Metadata (WithWhereDelete):
//   - Delete documents matching metadata criteria
//   - Uses same operators as queries: $eq, $ne, $gt, $gte, $lt, $lte, $in, $nin
//   - Example: {"category": {"$eq": "outdated"}}
//   - Useful for bulk cleanup operations
//
// 3. By Document Content (WithWhereDocumentDelete):
//   - Delete documents containing specific text patterns
//   - Operators: $contains, $not_contains
//   - Example: {"$contains": "deprecated"}
//   - Useful for content-based cleanup
//
// 4. Combinations:
//   - Can combine multiple filters with $and, $or operators
//   - Example: Delete old documents from specific category
//
// Important Notes:
// - At least ONE filter must be provided (prevents accidental full deletion)
// - To delete all documents, use explicit filter: {"id": {"$ne": ""}}
// - Deleted IDs can be reused for new documents
// - No error if specified IDs don't exist (idempotent operation)
//
// Index Updates:
// - HNSW index is immediately updated
// - Graph connections are rebalanced
// - Search performance unaffected after deletion
// - Index may be compacted after large deletions
//
// Performance:
// - Deletion by ID: O(1) per document + O(log N) for index update
// - Deletion by filter: O(N) scan + O(M log N) for M deletions
// - Batch deletions are more efficient than individual deletes
// - Large deletions (>10k docs) may take several seconds
//
// Atomicity:
// - All specified deletes succeed or none do
// - If any document fails to delete, entire operation rolls back
// - Safe for concurrent operations
//
// Use Cases:
// - Remove obsolete or outdated documents
// - Clean up test data
// - Implement data retention policies
// - Remove duplicates
// - Delete documents failing quality checks
//
// Safety Considerations:
// - Deletion is permanent (no soft-delete or undo)
// - Consider archiving important data before deletion
// - Use specific ID filters for critical operations
// - Test filters on small sets before bulk deletion
//
// Memory Management:
// - Freed memory is available for new documents
// - Storage space may not be immediately reclaimed (depends on backend)
// - Consider periodic collection recreation for optimal performance
//
// Parameters:
//   - collection: Target collection
//   - ids: Array of document IDs to delete
//
// Returns:
//   - error: If deletion fails or no filter provided
//
// Example:
//
//	// Delete specific documents
//	err := client.DeleteDocuments(collection, []string{"doc1", "doc2"})
//
//	// For metadata-based deletion, use collection.Delete directly:
//	// collection.Delete(ctx, chroma.WithWhereDelete(chroma.EqString("status", "deleted")))
func (c *Client) DeleteDocuments(collection chroma.Collection, ids []string) error {
	opts := []chroma.CollectionDeleteOption{
		chroma.WithIDsDelete(stringsToDocumentIDs(ids)...),
	}

	err := collection.Delete(c.ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to delete documents: %w", err)
	}
	return nil
}

// CountDocuments returns the total number of documents in a collection.
//
// ChromaDB API Details:
// - Endpoint: GET /api/v2/collections/{id}/count
// - Returns exact count of documents
// - Fast operation (O(1) complexity)
// - Count is maintained internally, no full scan required
//
// What Gets Counted:
// - All documents successfully added to the collection
// - Includes documents regardless of metadata
// - Excludes failed/partial adds
// - Updated in real-time with add/delete operations
//
// Use Cases:
// - Verify successful document additions
// - Monitor collection growth
// - Pagination calculations (total pages = count / page_size)
// - Resource planning (estimate memory/storage needs)
// - Data quality checks (expected vs actual count)
// - Progress tracking for bulk operations
//
// Performance:
// - Extremely fast (typically < 1ms)
// - No disk I/O required
// - Count cached in collection metadata
// - Safe to call frequently
//
// Accuracy:
// - Always accurate and up-to-date
// - Reflects committed transactions only
// - Consistent across all clients
//
// Parameters:
//   - collection: Collection to count documents in
//
// Returns:
//   - int: Total number of documents
//   - error: If count operation fails
//
// Example:
//
//	count, err := client.CountDocuments(collection)
//	if err == nil {
//	    fmt.Printf("Collection has %d documents\n", count)
//	}
func (c *Client) CountDocuments(collection chroma.Collection) (int, error) {
	count, err := collection.Count(c.ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count documents: %w", err)
	}
	return count, nil
}

// Close closes the client and releases resources.
//
// ChromaDB API Details:
// - Closes HTTP connections in the connection pool
// - Releases any cached data or resources
// - Gracefully terminates ongoing requests (if possible)
//
// Cleanup Behavior:
//   - HTTP connection pool is drained
//   - Pending requests are allowed to complete
//   - Subsequent operations on this client will fail
//   - Collections obtained from this client remain valid handles
//     (but operations on them require active client)
//
// Best Practices:
// - Always call Close() when done with client
// - Use defer immediately after successful client creation
// - Close before application shutdown
// - Don't reuse closed clients
//
// Note: Current implementation is a no-op as the chroma-go v2 client
// doesn't expose explicit cleanup methods. This method exists for:
// - API consistency and future compatibility
// - Clear ownership semantics
// - Potential future resource management needs
//
// Returns:
//   - error: Currently always nil, may change in future versions
//
// Example:
//
//	client, err := NewClient(ctx, "")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close() // Ensure cleanup
func (c *Client) Close() error {
	// The chroma-go v2 client doesn't currently expose a Close method
	// This is here for future compatibility and API consistency
	return nil
}

// stringsToDocumentIDs converts a slice of string IDs to ChromaDB DocumentID types.
//
// Helper Function Details:
// - DocumentID is a type alias for string in ChromaDB v2 API
// - Type conversion provides type safety
// - Prevents accidental mixing of IDs with other strings
// - Zero-cost abstraction (no runtime overhead)
//
// Purpose:
// - ChromaDB API uses DocumentID type for explicit type safety
// - Prevents common bugs (passing wrong string type)
// - Makes API intentions clearer
// - Enables future ID format changes without breaking API
//
// Parameters:
//   - ids: Slice of string IDs
//
// Returns:
//   - []chroma.DocumentID: Converted slice with proper type
func stringsToDocumentIDs(ids []string) []chroma.DocumentID {
	docIDs := make([]chroma.DocumentID, len(ids))
	for i, id := range ids {
		docIDs[i] = chroma.DocumentID(id)
	}
	return docIDs
}
