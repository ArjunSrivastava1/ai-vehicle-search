#!/usr/bin/env python3
"""
Embedding Service for AI Vehicle Search

Provides semantic search capabilities using HuggingFace's embeddinggemma-2 model.
Exposes an HTTP endpoint for computing cosine similarity between query and vehicle embeddings.

Usage:
    python embedding_service.py

Server will start on port 3001 and be accessible at http://localhost:3001
"""

import os
import sys
import json
import time
import logging
import threading
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from typing import Dict, List, Any, Optional
import urllib.parse

# Try to import transformers, handle graceful degradation
try:
    from transformers import AutoTokenizer, AutoModel
    import torch
    TRANSFORMERS_AVAILABLE = True
except ImportError:
    TRANSFORMERS_AVAILABLE = False
    print("Warning: transformers not available. Semantic search will use fallback.", file=sys.stderr)

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s [%(levelname)s] %(message)s'
)
logger = logging.getLogger(__name__)

# Configuration
HOST = os.getenv("EMBEDDING_SERVICE_HOST", "0.0.0.0")
PORT = int(os.getenv("EMBEDDING_SERVICE_PORT", "3001"))
MODEL_NAME = os.getenv("EMBEDDING_MODEL", "google/embeddinggemma-2")

class EmbeddingService:
    """Service that loads and manages the embedding model."""
    
    def __init__(self):
        self.model = None
        self.processor = None
        self.device = None
        self.loaded = False
        self.load_error = None
        self._inference_lock = threading.Lock()  # serialize model calls
        
        self._load_model()
    
    def _load_model(self):
        """Load the embedding model at startup."""
        if not TRANSFORMERS_AVAILABLE:
            self.load_error = "transformers library not available"
            return
        
        try:
            logger.info(f"Loading embedding model: {MODEL_NAME}")
            
            # Get device - prefer GPU if available
            if torch.cuda.is_available():
                self.device = "cuda"
                logger.info("Using CUDA GPU")
            elif torch.backends.mps.is_available():
                self.device = "mps"
                logger.info("Using MPS (Apple Silicon)")
            else:
                self.device = "cpu"
                logger.info("Using CPU")
            
            # Load processor and model
            self.processor = AutoTokenizer.from_pretrained(MODEL_NAME)
            self.model = AutoModel.from_pretrained(MODEL_NAME, dtype=torch.float32)
            self.model = self.model.to(self.device)
            self.model.eval()
            
            self.loaded = True
            logger.info(f"Model loaded successfully on {self.device}")
            
        except Exception as e:
            self.load_error = str(e)
            logger.error(f"Failed to load model: {e}")
    
    def encode(self, text: str) -> Optional[List[float]]:
        """
        Encode text to embedding vector.
        
        Args:
            text: Input text to encode
            
        Returns:
            List of floats representing the embedding vector, or None if failed
        """
        if not self.loaded or self.load_error:
            return None
        
        try:
            with self._inference_lock:
                inputs = self.processor(text, return_tensors="pt", padding=True, truncation=True)
                inputs = {k: v.to(self.device) for k, v in inputs.items()}
        
                with torch.no_grad():
                    embeddings = self.model(**inputs).last_hidden_state
        
                # Mean pool over all tokens
                mask = inputs["attention_mask"].squeeze(0)
                embeddings = embeddings.squeeze(0)
                masked_embeddings = embeddings * mask.unsqueeze(-1)
                token_sum = masked_embeddings.sum(dim=0)
                embedding = token_sum / mask.sum()
        
                result = embedding.tolist()
    
            return result
                
        except Exception as e:
            logger.error(f"Encoding error: {e}")
            return None
    
    def compute_similarity(self, query: str, vehicle_data: Dict[str, Any]) -> float:
        """
        Compute cosine similarity between query and vehicle representation.
        
        Args:
            query: User search query
            vehicle_data: Dictionary of vehicle attributes
            
        Returns:
            Cosine similarity score (0.0 to 1.0), or 0.5 if computation failed
        """
        if not self.loaded or self.load_error:
            return 0.5
        
        try:
            # Create vehicle description text
            vehicle_text = self._build_vehicle_description(vehicle_data)
            query_embedding = self.encode(query)
            vehicle_embedding = self.encode(vehicle_text)
            
            if query_embedding is None or vehicle_embedding is None:
                return 0.5
            
            # Compute cosine similarity
            query_vec = torch.tensor(query_embedding)
            vehicle_vec = torch.tensor(vehicle_embedding)
            
            # Normalize and compute cosine similarity
            query_norm = query_vec.norm()
            vehicle_norm = vehicle_vec.norm()
            
            if query_norm > 0 and vehicle_norm > 0:
                similarity = torch.dot(query_vec, vehicle_vec) / (query_norm * vehicle_norm)
                return float(similarity.item())
            
            return 0.5
            
        except Exception as e:
            logger.error(f"Similarity computation error: {e}")
            return 0.5
    
    def _build_vehicle_description(self, vehicle_data: Dict[str, Any]) -> str:
        """
        Build a natural language description from vehicle attributes.
        """
        parts = []
        
        # Name
        if "name" in vehicle_data:
            parts.append(f"{vehicle_data['name']}")
        
        # Year
        if "year" in vehicle_data and vehicle_data["year"]:
            parts.append(f"{vehicle_data['year']}")
        
        # Price
        if "price" in vehicle_data:
            price = vehicle_data["price"]
            if price > 1000000:  # Likely lakhs
                parts.append(f"₹{int(price/100000)} lakhs")
            else:
                parts.append(f"₹{int(price)}")
        
        # Fuel
        if "fuel" in vehicle_data:
            fuel_map = {
                "petrol": "Petrol",
                "diesel": "Diesel",
                "ev": "Electric",
                "cng": "CNG",
                "hybrid": "Hybrid"
            }
            parts.append(fuel_map.get(vehicle_data["fuel"], vehicle_data["fuel"]))
        
        # Transmission
        if "transmission" in vehicle_data:
            trans_map = {
                "manual": "Manual",
                "automatic": "Automatic",
                "amt": "AMT"
            }
            parts.append(trans_map.get(vehicle_data["transmission"], vehicle_data["transmission"]))
        
        # Body Type
        if "body_type" in vehicle_data:
            body_map = {
                "sedan": "Sedan",
                "suv": "SUV",
                "hatchback": "Hatchback",
                "wagon": "Wagon",
                "mpv": "MPV",
                "van": "Van",
                "truck": "Truck"
            }
            parts.append(body_map.get(vehicle_data["body_type"], vehicle_data["body_type"]))
        
        # Seats
        if "seats" in vehicle_data and vehicle_data["seats"]:
            parts.append(f"{vehicle_data['seats']} seats")
        
        # Safety Rating
        if "safety_rating" in vehicle_data and vehicle_data["safety_rating"]:
            parts.append(f"{vehicle_data['safety_rating']} safety rating")
        
        return " ".join(parts)


class EmbeddingAPIHandler(BaseHTTPRequestHandler):
    """HTTP request handler for the embedding service."""
    
    service = None  # Will be set by the main function
    
    def log_message(self, format, *args):
        """Suppress default logging, use our logger instead."""
        pass
    
    def send_json_response(self, data: Dict[str, Any], status: int = 200):
        """Send a JSON response."""
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(data, indent=2).encode())
    
    def do_GET(self):
        """Handle GET requests."""
        parsed_path = urllib.parse.urlparse(self.path)
        
        if parsed_path.path == "/health":
            self._handle_health()
        elif parsed_path.path == "/status":
            self._handle_status()
        elif parsed_path.path == "/":
            self._handle_root()
        else:
            self.send_json_response({"error": "Not found"}, 404)
    
    def do_POST(self):
        """Handle POST requests."""
        parsed_path = urllib.parse.urlparse(self.path)
        
        if parsed_path.path == "/embed":
            self._handle_embed()
        elif parsed_path.path == "/similar":
            self._handle_similarity()
        else:
            self.send_json_response({"error": "Not found"}, 404)
    
    def _handle_root(self):
        """Root endpoint - show API info."""
        self.send_json_response({
            "service": "Embedding Service",
            "version": "1.0.0",
            "model": MODEL_NAME,
            "status": "loaded" if self.service and self.service.loaded else "not loaded",
            "endpoints": {
                "GET /health": "Check service health",
                "GET /status": "Get model status",
                "POST /embed": "Encode text to embedding",
                "POST /similar": "Compute similarity between query and vehicle"
            }
        })
    
    def _handle_status(self):
        """Return detailed status information."""
        if not self.service:
            self.send_json_response({
                "status": "unavailable",
                "error": "Service not initialized"
            }, 503)
            return
        
        data = {
            "status": "loaded" if self.service.loaded else "failed",
            "model": MODEL_NAME,
            "device": self.service.device if self.service.loaded else None,
            "loaded": self.service.loaded,
            "load_error": self.service.load_error if self.service.load_error else None,
            "uptime_seconds": time.time() - self.start_time if hasattr(self, 'start_time') else 0
        }
        self.send_json_response(data)
    
    def _handle_health(self):
        """Health check endpoint."""
        if not self.service:
            self.send_json_response({
                "status": "unavailable",
                "healthy": False,
                "error": "Service not initialized"
            }, 503)
            return
        
        if not self.service.loaded:
            self.send_json_response({
                "status": "unhealthy",
                "healthy": False,
                "error": self.service.load_error
            }, 503)
            return
        
        # Quick sanity check - encode a test string
        test_embedding = self.service.encode("test")
        if test_embedding is None:
            self.send_json_response({
                "status": "unhealthy",
                "healthy": False,
                "error": "Model encoding failed"
            }, 503)
            return
        
        self.send_json_response({
            "status": "healthy",
            "healthy": True,
            "model": MODEL_NAME,
            "device": self.service.device
        }, 200)
    
    def _handle_embed(self):
        """Encode a text string to an embedding vector."""
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length).decode()
        
        try:
            data = json.loads(body)
            text = data.get("text", "")
            
            if not text:
                self.send_json_response({"error": "text field is required"}, 400)
                return
            
            embedding = self.service.encode(text)
            
            if embedding is None:
                self.send_json_response({
                    "text": text,
                    "embedding": None,
                    "error": self.service.load_error if self.service.load_error else "Model not available"
                }, 500)
                return
            
            self.send_json_response({
                "text": text,
                "embedding": embedding,
                "dimension": len(embedding)
            })
            
        except json.JSONDecodeError as e:
            self.send_json_response({"error": f"Invalid JSON: {e}"}, 400)
        except Exception as e:
            self.send_json_response({"error": str(e)}, 500)
    
    def _handle_similarity(self):
        """Compute similarity between query and vehicle."""
        content_length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(content_length).decode()
        
        try:
            data = json.loads(body)
            query = data.get("query", "")
            vehicle_data = data.get("vehicle", {})
            
            if not query:
                self.send_json_response({"error": "query field is required"}, 400)
                return
            
            if not vehicle_data:
                self.send_json_response({"error": "vehicle field is required"}, 400)
                return
            
            similarity = self.service.compute_similarity(query, vehicle_data)
            
            self.send_json_response({
                "query": query,
                "vehicle": vehicle_data,
                "similarity": similarity
            })
            
        except json.JSONDecodeError as e:
            self.send_json_response({"error": f"Invalid JSON: {e}"}, 400)
        except Exception as e:
            self.send_json_response({"error": str(e)}, 500)


def main():
    """Start the embedding service."""
    logger.info("=" * 60)
    logger.info("Starting Embedding Service")
    logger.info(f"Model: {MODEL_NAME}")
    logger.info(f"Host: {HOST}, Port: {PORT}")
    logger.info("=" * 60)
    
    service = EmbeddingService()
    
    # Store service instance and startup time
    EmbeddingAPIHandler.service = service
    handler = EmbeddingAPIHandler
    server = ThreadingHTTPServer((HOST, PORT), handler)
    server.daemon_threads = True
    server.timeout = None
    
    start_time = time.time()
    handler.start_time = start_time
    
    logger.info(f"Embedding service running on http://{HOST}:{PORT}")
    
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Shutting down...")
        server.shutdown()


if __name__ == "__main__":
    main()
