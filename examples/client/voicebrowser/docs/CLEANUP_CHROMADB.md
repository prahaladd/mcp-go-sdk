# ChromaDB Cleanup Script

## 📝 Description

A utility script to delete all collections from ChromaDB. Useful for:
- Starting fresh before testing
- Cleaning up after failed runs
- Removing old session data

## 🚀 Usage

```bash
# From the voicebrowser directory
go run cleanup_chromadb.go
```

Or build it once and reuse:

```bash
# Build the cleanup tool
go build -o cleanup_chromadb cleanup_chromadb.go

# Run it
./cleanup_chromadb
```

## 📊 Example Output

### When collections exist:
```
🔍 Listing all ChromaDB collections...
Found 2 collection(s):
  - voicebrowser-session-1766256759
  - voicebrowser-session-1766257000

🗑️  Deleting all collections...
  ✓ Deleted: voicebrowser-session-1766256759
  ✓ Deleted: voicebrowser-session-1766257000

✅ ChromaDB cleanup complete! Deleted 2 collection(s).
```

### When already clean:
```
🔍 Listing all ChromaDB collections...
✓ No collections found - ChromaDB is already clean!
```

## 🔧 Requirements

- ChromaDB running on `http://localhost:8000`
- Go modules initialized (`go mod tidy` already run)

## ⚠️ Warning

**This script deletes ALL collections in ChromaDB!**

If you have other collections you want to keep, do NOT run this script.
Consider manually deleting specific collections instead.

## 💡 When to Use

**Before testing:**
```bash
# Clean slate for new test
go run cleanup_chromadb.go
./voicebrowser --execution-mode chromadb-driven --file workflow.txt
```

**After failed runs:**
```bash
# Remove incomplete/corrupted data
go run cleanup_chromadb.go
```

**Regular maintenance:**
```bash
# Clean up old sessions weekly
go run cleanup_chromadb.go
```

## 🛠️ Troubleshooting

### "Failed to connect to ChromaDB"
**Cause:** ChromaDB is not running

**Solution:**
```bash
docker run -p 8000:8000 chromadb/chroma
```

### "Failed to list collections"
**Cause:** ChromaDB API version mismatch or connection issue

**Solution:**
- Check ChromaDB is accessible: `curl http://localhost:8000/api/v1/heartbeat`
- Restart ChromaDB container
- Check firewall/port settings

## 📁 File Location

`/home/crazyjarvis/projects/gomcptool/mcp-go-sdk/examples/client/voicebrowser/cleanup_chromadb.go`

---

**Quick Command Reference:**
```bash
# Run cleanup
go run cleanup_chromadb.go

# Build + run
go build -o cleanup_chromadb cleanup_chromadb.go && ./cleanup_chromadb

# Check ChromaDB is running
curl http://localhost:8000/api/v1/heartbeat
```
