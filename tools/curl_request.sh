
#!/bin/bash

# Check if a filename was provided
if [ $# -lt 1 ]; then
    echo "Usage: $0 <input_file>"
    echo "Example: $0 instructions.txt"
    exit 1
fi

# Read the file content
INPUT_FILE="$1"
if [ ! -f "$INPUT_FILE" ]; then
    echo "Error: File '$INPUT_FILE' not found."
    exit 1
fi

# Read file content into a variable
FILE_CONTENT=$(cat "$INPUT_FILE")

# Escape special characters in the content for JSON
ESCAPED_CONTENT=$(echo "$FILE_CONTENT" | sed 's/\\/\\\\/g' | sed 's/"/\\"/g' | sed 's/\n/\\n/g' | sed ':a;N;$!ba;s/\n/\\n/g')

curl "https://api.openai.com/v1/chat/completions" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $OPENAI_API_KEY" \
    -d '{
  "model": "gpt-4",
  "messages": [
    {
      "role": "system",
      "content": "You are an assistant that helps users navigate and control a browser using MCP tools. You MUST use the browser tools provided to you when the user asks for anything related to browsing. Don't just describe what you would do - actually use the tools."
    },
    {
      "role": "user",
      "content": "Here's a document that contains a numbered sequence of steps between {steps} and {/steps} delimiters, that require to be automated.\n\n{steps}We need to perform the below set of steps to create images using CanvaAI.\n\n1. **Navigate to Canva.com**  \n   - Open the browser and go to [https://www.canva.com](https://www.canva.com).  \n   - Ensure the page is fully loaded before proceeding.\n\n2. **Perform Login**  \n   - First check if the user is already logged in. We can confirm this by checking the selector for the `Canva AI` button which is present within the attached JSON file. If the user has not already logged in then locate and click on the `Log In` button. \n        - Enter the required credentials (email and password) in the respective fields.  \n        - Click the \"Log In\" or \"Submit\" button to complete the login process. \n   \n   - Wait for the dashboard or homepage to load completely. Again check for the selector corresponding to the `Canva AI` button.\n\n3. **Click on the `Canva AI` Button**  \n   - Locate the button labeled `Canva AI`.  \n   - Click on it to open the Canva AI interface.  \n   - Wait for the Canva AI interface to load completely.\n\n4. **Enter Text in the Text Box**  \n   - Locate the text box in the Canva AI interface.  \n   - Enter the text `Moonlit Sunset` into the text box.\n\n5. **Click the `-\u003e` Button**  \n   - Locate the button labeled `-\u003e`.  \n   - Click on it to submit the input.  \n   - Wait for the response or output to load.\n\n{/steps}\n\nAnalyze one step at a time and return the next step to be performed.Think step by step. Restrict the tools to the  list provided in the request.\n (Please use the browser tools provided to help with this task)"
    }
  ],
  "temperature": 0.2,
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "browser_navigate",
        "description": "Navigate to a URL",
        "parameters": {
          "$schema": "http://json-schema.org/draft-07/schema#",
          "additionalProperties": false,
          "properties": {
            "url": {
              "description": "The URL to navigate to",
              "type": "string"
            }
          },
          "required": [
            "url"
          ],
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "browser_snapshot",
        "description": "Capture accessibility snapshot of the current page. Use this for getting references to elements to interact with.",
        "parameters": {
          "$schema": "http://json-schema.org/draft-07/schema#",
          "additionalProperties": false,
          "properties": {},
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "browser_click",
        "description": "Perform click on a web page",
        "parameters": {
          "$schema": "http://json-schema.org/draft-07/schema#",
          "additionalProperties": false,
          "properties": {
            "element": {
              "description": "Human-readable element description used to obtain permission to interact with the element",
              "type": "string"
            },
            "ref": {
              "description": "Exact target element reference from the page snapshot",
              "type": "string"
            }
          },
          "required": [
            "element",
            "ref"
          ],
          "type": "object"
        }
      }
    },
    {
      "type": "function",
      "function": {
        "name": "browser_type",
        "description": "Type text into editable element",
        "parameters": {
          "$schema": "http://json-schema.org/draft-07/schema#",
          "additionalProperties": false,
          "properties": {
            "element": {
              "description": "Human-readable element description used to obtain permission to interact with the element",
              "type": "string"
            },
            "ref": {
              "description": "Exact target element reference from the page snapshot",
              "type": "string"
            },
            "submit": {
              "description": "Whether to submit entered text (press Enter after)",
              "type": "boolean"
            },
            "text": {
              "description": "Text to type into the element",
              "type": "string"
            }
          },
          "required": [
            "element",
            "ref",
            "text",
            "submit"
          ],
          "type": "object"
        }
      }
    }
  ],
  "tool_choice": "required"
}'