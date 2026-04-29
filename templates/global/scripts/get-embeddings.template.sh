#!/usr/bin/env bash
# ai-dev-workflow bridge script for generating embeddings.
# This script is called by 'aidw memory' to generate vector embeddings.
# It should read the input text from STDIN and output a JSON array of floats to STDOUT.
#
# Default implementation: Gemini embedding-004 via Google Cloud Vertex AI (OAuth).
# Customize this script to use OpenAI, Anthropic, or any other provider.

set -euo pipefail

# 1. Get OAuth Access Token (requires gcloud auth)
# If you use a different auth method, update this line.
ACCESS_TOKEN=$(gcloud auth print-access-token 2>/dev/null || echo "")

if [[ -z "$ACCESS_TOKEN" ]]; then
  echo "ERROR: gcloud auth access token not found. Run 'gcloud auth login' or 'gcloud auth application-default login'." >&2
  exit 1
fi

# 2. Project and Location (customize these)
PROJECT_ID=$(gcloud config get-value project 2>/dev/null || echo "my-project")
LOCATION="us-central1"

# 3. Read input text from STDIN
TEXT=$(cat)

# 4. Call Vertex AI Embedding API
# Ref: https://cloud.google.com/vertex-ai/docs/generative-ai/embeddings/get-text-embeddings
curl -s -X POST \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json; charset=utf-8" \
  "https://${LOCATION}-aiplatform.googleapis.com/v1/projects/${PROJECT_ID}/locations/${LOCATION}/publishers/google/models/text-embedding-004:predict" \
  -d "{
    'instances': [
      {'content': '$TEXT'}
    ]
  }" | jq -c '.predictions[0].embeddings.values'
