# SSE MCP Test Server

This is a test server for verifying external MCP functionality in SSE mode.

## Usage

### 1. Start the test server

```bash
cd cmd/test-sse-mcp-server
go run main.go
```

The server will start at `http://127.0.0.1:8082` and expose the following endpoints:
- `GET /sse` - SSE event stream endpoint
- `POST /message` - message receiving endpoint

### 2. Add the configuration in CyberStrikeAI

Add an external MCP configuration in the web interface using the following JSON:

```json
{
  "test-sse-mcp": {
    "transport": "sse",
    "url": "http://127.0.0.1:8082/sse",
    "description": "SSE MCP test server",
    "timeout": 30
  }
}
```

### 3. Test the features

The test server provides two test tools:

1. **test_echo** - Echoes the input text back
   - Parameter: `text` (string) - the text to echo

2. **test_add** - Computes the sum of two numbers
   - Parameter: `a` (number) - the first number
   - Parameter: `b` (number) - the second number

## How It Works

1. The client establishes an SSE connection via `GET /sse` and receives events pushed by the server
2. The client sends MCP protocol messages via `POST /message`
3. After the server processes a message, it pushes the response over the SSE connection

## Logs

The server outputs the following log entries:
- SSE client connection/disconnection
- Received requests (method name and ID)
- Tool call details
