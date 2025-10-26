# Element Discovery Workflow for Form Filling

## The Problem
When testing form filling capabilities, you need to know what elements are available on the page and how to target them.

## The Solution: Use aria-snapshot for Discovery

### Step 1: Navigate to the page
```bash
./cdpbrowser-client navigate https://google.com
```

### Step 2: Discover interactive elements
```bash
./cdpbrowser-client aria-snapshot llm-text interactive
```

This will output something like:
```
PAGE: Google (https://google.com)

INTERACTIVE ELEMENTS:
• [text] "Search" (selector: textarea[name="q"])
• [button] "Google Search" (selector: input[name="btnK"])
• [button] "I'm Feeling Lucky" (selector: input[name="btnI"])
• [link] "Gmail" (selector: a[href="https://mail.google.com/mail/"])
• [link] "Images" (selector: a[href="/imghp"])
```

### Step 3: Use discovered selectors for form filling

Now you can use any of these targeting strategies:

#### Option 1: Use the CSS selector from aria-snapshot
```bash
./cdpbrowser-client type-text "textarea[name='q']" "hello world"
./cdpbrowser-client click-button "input[name='btnK']"
```

#### Option 2: Use ARIA label (smart selector)
```bash
./cdpbrowser-client type-text "Search" "hello world"
./cdpbrowser-client click-button "Google Search"
```

#### Option 3: Use DOM attributes
```bash
./cdpbrowser-client type-text "[name='q']" "hello world"
./cdpbrowser-client click-button "[name='btnK']"
```

#### Option 4: Use DOM ID (if available)
```bash
./cdpbrowser-client type-text "#search-input" "hello world"
```

## Smart Selector Features

Our smart selector system tries multiple strategies in order:
1. Direct CSS selector match
2. DOM ID matching (with or without # prefix)
3. ARIA label exact match
4. ARIA label partial match (case-insensitive)
5. Text content matching for buttons/links
6. Placeholder text matching for inputs

This means you can use natural language selectors like:
- "Search" instead of "textarea[name='q']"
- "Sign in" instead of "#signin-button"
- "Email" instead of "input[type='email']"

## Interactive Discovery Workflow

For interactive testing:

```bash
./cdpbrowser-client interactive
```

Then:
```
cdp> navigate https://google.com
cdp> aria-snapshot llm-text interactive
cdp> type-text "Search" "MCP browser automation"
cdp> click-button "Google Search"
cdp> screenshot
cdp> exit
```

## Script-based Testing

Create a script file (like google-form-test.txt):
```
navigate https://google.com
wait 2
aria-snapshot llm-text interactive
type-text "Search" "test query"
click-button "Google Search"
wait 3
screenshot
```

Run with:
```bash
./cdpbrowser-client run-script google-form-test.txt
```

## Available Form Filling Tools

- `type-text <selector> <text> [clear]` - Type text into input fields
- `click-button <selector>` - Click button elements
- `click-link <selector>` - Click link elements  
- `select-dropdown <selector> <value>` - Select dropdown options
- `choose-option <selector> [checked]` - Check/uncheck radio buttons and checkboxes
- `refresh` - Refresh the current page

All tools support smart element targeting with fallback strategies.