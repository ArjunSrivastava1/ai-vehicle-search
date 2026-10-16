#!/bin/bash
# Start unsloth embedding server for bge-small-en-v1.5

MODEL="bge-small-en-v1.5"
PORT=8081

echo "Starting unsloth embedding server with $MODEL on port $PORT..."

# Check if server is already running
if netstat -an 2>/dev/null | grep -q ":$PORT.*LISTEN"; then
    echo "Server already running on port $PORT"
    exit 0
fi

# Start the server
unsloth serve --model "$MODEL" --port "$PORT" --gpu-memory-utilization 0.9 &
SERVER_PID=$!

echo "Server started with PID $SERVER_PID"

# Wait for server to be ready
echo "Waiting for server to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:$PORT/_health > /dev/null 2>&1; then
        echo "Server is ready!"
        exit 0
    fi
    sleep 1
done

echo "Server failed to start within timeout"
kill $SERVER_PID 2>/dev/null
exit 1
