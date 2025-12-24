# Quick Start: ChromaDB-Driven Mode

## 🚀 Get Started in 3 Steps

### **Step 1: Start ChromaDB**
```bash
docker run -p 8000:8000 chromadb/chroma
```

### **Step 2: Create Your Workflow**
Create a text file with your automation instructions:

```bash
echo "Navigate to Google.com and search for 'artificial intelligence'" > my_workflow.txt
```

### **Step 3: Run VoiceBrowser**
```bash
./voicebrowser \
  --execution-mode chromadb-driven \
  --env .vscode/voicebrowser.env \
  --file my_workflow.txt
```

---

## 📝 Workflow Examples

### **Example 1: Simple Search**
```
Navigate to Google.com and search for "ChromaDB"
```

### **Example 2: Multi-Step**
```
Go to GitHub.com
Click the search box
Type "mcp-go-sdk" and search
Click the first repository link
Take a screenshot
```

### **Example 3: Form Interaction**
```
Navigate to example.com/login
Type "user@example.com" in the email field
Type "password123" in the password field
Click the login button
Wait for the page to load and take a screenshot
```

---

## 💡 How It Works

### **User Provides Instructions** (Brief or Detailed)
```
"Search Google for AI"
```

### **LLM Generates Step Plan** (One-Time Call)
```
[TOOL:navigate] Navigate to https://www.google.com
[TOOL:aria_snapshot] Take snapshot of the page
[TOOL:type_text] Type "AI" into the search box
[TOOL:click_button] Click the Google Search button
[TOOL:screenshot] Take a screenshot
```

### **ChromaDB Finds Elements** (No More LLM!)
```
Step: "Type 'AI' into search box"
  → ChromaDB Query: "search box"
  → Found: [input] name="q"
  → Execute: type_text(selector='input[name="q"]', text='AI')
```

---

## 🆚 When to Use Each Mode

### **Use ChromaDB-Driven Mode When:**
- ✅ You have clear, repeatable workflows
- ✅ You want to minimize costs (90% token savings)
- ✅ You need fast, deterministic execution
- ✅ You're running production automation

### **Use LLM-Driven Mode When:**
- ✅ Exploring new websites
- ✅ Need dynamic decision-making
- ✅ Complex conditional logic required
- ✅ Debugging or prototyping

---

## 🔧 All Command-Line Options

```bash
./voicebrowser \
  --execution-mode chromadb-driven \     # Use ChromaDB mode (default: llm-driven)
  --chromadb http://localhost:8000 \     # ChromaDB URL (default: localhost:8000)
  --enable-chromadb=true \               # Enable ChromaDB (default: true)
  --file my_workflow.txt \               # Workflow instructions file
  --env .vscode/voicebrowser.env \       # API keys file
  --cdpbrowser ./cdpbrowser              # Browser server path
```

---

## 📊 Expected Output

```
VoiceBrowser: OpenAI-powered browser automation
🔄 Using ChromaDB-driven execution mode

Initializing ChromaDB connection...
✓ ChromaDB connected: http://localhost:8000
✓ Collection: voicebrowser-session-1734716394

📋 Step 1: Generating execution plan from user instructions...

✅ Generated 5 steps:
  1. [navigate] Navigate to https://www.google.com
  2. [aria_snapshot] Take snapshot of the page
  3. [type_text] Type "AI" into the search box
  4. [click_button] Click the Google Search button
  5. [screenshot] Take a screenshot

🚀 Step 2: Executing plan with ChromaDB-driven element selection...

▶️  Executing Step 1/5: [navigate] Navigate to https://www.google.com
  🌐 Navigating to: https://www.google.com
  📸 Auto-taking ARIA snapshot to populate ChromaDB...
  ✅ ChromaDB populated with 47 elements
✅ Step 1 completed

▶️  Executing Step 2/5: [type_text] Type "AI" into the search box
  🔍 Querying ChromaDB for: 'search box'
  ✓ Found: [input] 'Search' → input[name="q"]
  ⌨️  Typing 'AI' into element: input[name="q"]
✅ Step 2 completed

🎉 Workflow completed successfully!
```

---

## ❓ Troubleshooting

### **"ChromaDB collection not initialized"**
- Ensure `--enable-chromadb=true`
- Check ChromaDB is running: `curl http://localhost:8000/api/v1/heartbeat`

### **"No matching elements found in ChromaDB"**
- The page might not have been snapshotted yet
- Navigation steps automatically snapshot the page
- Add manual `[TOOL:aria_snapshot]` step if needed

### **"Could not extract URL from description"**
- Make sure navigate steps include full URLs
- Example: `Navigate to https://www.google.com` (not just "google")

---

## 💰 Cost Savings Example

### **Traditional LLM-Driven Mode:**
- 15 LLM API calls × 2,000 tokens = 30,000 tokens
- Cost: ~$1.50 (GPT-4o pricing)

### **ChromaDB-Driven Mode:**
- 1 LLM API call × 1,000 tokens = 1,000 tokens
- Cost: ~$0.05 (GPT-4o pricing)

**Savings: 97% reduction in costs!**

---

## 🎯 Best Practices

1. **Be Specific**: "Click the blue Submit button" vs "Click submit"
2. **Include URLs**: Use full URLs in navigate instructions
3. **One Action Per Step**: "Type X" then "Click Y" (not "Type X and click Y")
4. **Test First**: Run with `--execution-mode llm-driven` to verify workflow
5. **Check ChromaDB**: Ensure ChromaDB is populated after navigation

---

## 📚 Learn More

- **Full Documentation**: `CHROMADB_INTEGRATION.md`
- **Implementation Details**: `IMPLEMENTATION_SUMMARY.md`
- **Original Features**: Original README.md

---

**Ready to save 90% on LLM costs? Start using ChromaDB-driven mode today!** 🚀
