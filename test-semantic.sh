#!/bin/bash
# Test script for semantic search

echo "Starting server..."
go run cmd/server/main.go &
SERVER_PID=$!
echo "Server started with PID $SERVER_PID"

# Wait for server to be ready
echo "Waiting for server to be ready..."
for i in {1..30}; do
    if curl -s http://localhost:8080 > /dev/null 2>&1; then
        echo "Server is ready!"
        break
    fi
    sleep 1
done

# Test queries
echo ""
echo "Test 1: Exact filter (Diesel, Automatic)"
curl -s -X POST http://localhost:8080/search/filter \
  -H "Content-Type: application/json" \
  -d '{"fuel": "Diesel", "transmission": "Automatic"}' | jq '.'

echo ""
echo "Test 2: Budget-focused (under 800000)"
curl -s -X POST http://localhost:8080/search/filter \
  -H "Content-Type: application/json" \
  -d '{"price_max": 800000}' | jq '.'

echo ""
echo "Test 3: Freshness-focused (year >= 2022)"
curl -s -X POST http://localhost:8080/search/filter \
  -H "Content-Type: application/json" \
  -d '{"year_min": 2022}' | jq '.'

echo ""
echo "Test 4: Semantic (cheap old diesel car)"
curl -s -X POST http://localhost:8080/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "cheap old diesel car"}' | jq '.'

echo ""
echo "Test 5: Complex (new premium SUV for family)"
curl -s -X POST http://localhost:8080/search/semantic \
  -H "Content-Type: application/json" \
  -d '{"query": "new premium SUV for family"}' | jq '.'

# Cleanup
echo ""
echo "Stopping server..."
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

echo "Done!"
