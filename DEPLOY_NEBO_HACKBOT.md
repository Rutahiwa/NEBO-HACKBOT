# NEBO-HACKBOT Deployment Guide

GPU VM deployment for NEBO-HACKBOT on Debian 13 with 2x NVIDIA A100 40GB.

---

## Prerequisites

| Requirement | Detail |
|---|---|
| OS | Debian 13 (trixie) or later |
| GPU | 2x NVIDIA A100 40GB (80 GB total VRAM) |
| Docker | Docker Engine 24+ with Docker Compose v2 |
| NVIDIA Container Toolkit | `nvidia-ctk` installed and configured so `docker run --gpus all` works |
| Disk | At least 80 GB free for model weights + container images |
| RAM | 64 GB+ recommended (model loading spills to system memory briefly) |

Verify GPU access before proceeding:

```bash
# Should list both A100 GPUs
nvidia-smi

# Should print CUDA info inside the container
docker run --rm --gpus all nvidia/cuda:12.4.1-base-ubuntu22.04 nvidia-smi
```

---

## Step 1: vLLM Serving Setup (recommended over Ollama)

vLLM is the recommended inference engine for this deployment. It outperforms Ollama in every dimension that matters for NEBO-HACKBOT's multi-agent architecture:

- **Tensor parallelism across both A100s.** Ollama does not shard a single model across GPUs; it either runs on one GPU or loads separate models per GPU. vLLM splits Qwen2.5-72B across both cards, giving the model the full 80 GB VRAM budget.
- **Automatic prefix caching.** NEBO-HACKBOT's system prompts are stable across agent calls. vLLM's prefix caching reuses the KV-cache for the shared prompt prefix, saving 30-50% of per-call compute on long system prompts.
- **Continuous batching.** When multiple specialist agents (recon, injection, XSS, etc.) run concurrently, vLLM batches their requests at the attention level rather than queuing them sequentially.
- **PagedAttention.** Efficient memory management for long-context chains (32K tokens), avoiding the fragmentation that causes OOM on naive KV-cache allocation.

### Start the vLLM server

```bash
docker run -d --gpus all \
  --name vllm-server \
  -p 8000:8000 \
  vllm/vllm-openai:latest \
  --model Qwen/Qwen2.5-72B-Instruct-AWQ \
  --tensor-parallel-size 2 \
  --max-model-len 32768 \
  --enable-prefix-caching \
  --gpu-memory-utilization 0.90 \
  --port 8000
```

**Flag reference:**

| Flag | Purpose |
|---|---|
| `--tensor-parallel-size 2` | Shard the model across both A100s |
| `--max-model-len 32768` | Allow 32K context windows for long agent chains |
| `--enable-prefix-caching` | Reuse KV-cache for stable system prompt prefixes |
| `--gpu-memory-utilization 0.90` | Reserve 90% of each GPU for model/cache; leave 10% headroom for CUDA allocator |

### Verify the server is ready

```bash
# Wait for the model to load (takes 2-5 minutes on first pull)
docker logs -f vllm-server

# Test the endpoint
curl http://localhost:8000/v1/models
# Should return: {"data": [{"id": "Qwen/Qwen2.5-72B-Instruct-AWQ", ...}]}
```

---

## Step 2: NEBO-HACKBOT Stack

### Clone and configure

```bash
git clone https://github.com/<org>/nebo-hackbot.git
cd nebo-hackbot
cp .env.example .env
```

### Configure the LLM provider

Edit `.env` to point at the vLLM server using the `custom` provider type. The custom provider speaks the OpenAI-compatible API that vLLM exposes.

```bash
# --- LLM Provider Configuration ---
# Use the "custom" provider type for vLLM's OpenAI-compatible endpoint
LLM_SERVER_URL=http://localhost:8000/v1
LLM_SERVER_MODEL=Qwen/Qwen2.5-72B-Instruct-AWQ
LLM_SERVER_API_KEY=not-needed

# Alternatively, if using the "openai" provider type:
# OPENAI_API_KEY=not-needed
# OPENAI_BASE_URL=http://localhost:8000/v1
# OPENAI_MODEL=Qwen/Qwen2.5-72B-Instruct-AWQ
```

**Notes on provider selection:**

- The `custom` provider (`LLM_SERVER_URL` + `LLM_SERVER_MODEL`) is the cleanest option. It requires `LLM_SERVER_URL` to be non-empty and either `LLM_SERVER_MODEL` or `LLM_SERVER_CONFIG` to be set. See `backend/pkg/providers/registry.go` for the exact gating logic.
- vLLM does not validate API keys by default, so set `LLM_SERVER_API_KEY` to any non-empty string (the SDK may require a non-empty value).
- If you later want to use a cloud provider for comparison testing, you can add its API key alongside the custom config. NEBO-HACKBOT supports multiple simultaneous providers.

### Start the stack

```bash
# Start all services (PostgreSQL, backend, frontend, etc.)
docker compose up -d

# Verify all containers are running
docker compose ps
```

The web UI will be available at `https://localhost:8443` once the stack is up.

### Optional: observability stack

```bash
# Add Grafana + VictoriaMetrics + Loki + Jaeger
docker compose -f docker-compose.yml -f docker-compose-observability.yml up -d

# Add Langfuse for LLM analytics (token usage, latency tracking)
docker compose -f docker-compose.yml -f docker-compose-langfuse.yml up -d
```

---

## Step 3: New Environment Variables

The NEBO-HACKBOT fork adds the following environment variables (defined in `backend/pkg/config/config.go`):

| Variable | Default | Description |
|---|---|---|
| `USE_CONTEXT_WINDOW` | `true` | Enables sliding-window context management. When true, each agent call sees only the system prompt prefix plus the last N conversation messages, preventing unbounded context growth. Set to `false` to revert to the original PentAGI behavior (full chain sent every call). |
| `CONTEXT_WINDOW_SIZE` | `10` | Number of trailing messages retained in the context window. Only effective when `USE_CONTEXT_WINDOW=true`. Smaller values reduce token usage but may lose recent context. Values of 5-15 are reasonable; 10 is a good default for Qwen2.5-72B at 32K context. |

Add these to your `.env` file:

```bash
# --- NEBO-HACKBOT Context Management ---
USE_CONTEXT_WINDOW=true
CONTEXT_WINDOW_SIZE=10
```

---

## Rollback

If you need to revert to the original PentAGI behavior:

### Disable context windowing

```bash
# In .env:
USE_CONTEXT_WINDOW=false
```

This single change disables the sliding-window context system. The backend will send the full message chain to the LLM on every call, exactly as upstream PentAGI does. No restart of vLLM is needed; only restart the backend container:

```bash
docker compose restart backend
```

### Switch to Ollama (if needed)

If you want to revert the inference engine to Ollama:

```bash
# Stop vLLM
docker stop vllm-server && docker rm vllm-server

# In .env, replace the custom provider config with Ollama:
# LLM_SERVER_URL=          (clear or comment out)
# LLM_SERVER_MODEL=        (clear or comment out)
OLLAMA_SERVER_URL=http://localhost:11434

# Restart the stack
docker compose up -d
```

Note: Ollama will only use a single GPU for inference (no tensor parallelism), so expect significantly slower response times with 72B parameter models.

### Fallback agent path

The original `pentester` agent (tool name: `pentester`, prompt: `pentester.tmpl`) still exists in the codebase and works as a fallback. The specialist agents (recon, injection, xss, auth, idor, ssrf) and the validator agent are additive; they do not replace the pentester agent. If the Primary Agent does not delegate to specialists, it falls back to the pentester for general-purpose security testing.

---

## Scope Enforcement (Future Phase)

Target scope enforcement is planned for a future phase. When implemented, it will add allowlist validation in the following locations:

| File | Enforcement Point |
|---|---|
| `backend/pkg/tools/terminal.go` | Validate that target IPs/hostnames in shell commands match the declared scope before execution in the sandbox container |
| `backend/pkg/tools/browser.go` | Validate that URLs passed to the headless browser are within the scope allowlist |
| `backend/pkg/docker/client.go` | Apply Docker network policies on sandbox containers to restrict outbound traffic to in-scope targets only |

Until scope enforcement is implemented, operators must review agent actions and ensure the target specification in the flow prompt is precise. The agents generally respect the target given in the prompt, but there is no hard enforcement at the infrastructure level yet.

---

## Architecture Summary

```
                  +-------------------+
                  |   Web UI (:8443)  |
                  +--------+----------+
                           |
                  +--------v----------+
                  |  NEBO-HACKBOT     |
                  |  Backend (Go)     |
                  |                   |
                  |  Primary Agent    |
                  |    |-- Recon      |
                  |    |-- Injection  |
                  |    |-- XSS        |
                  |    |-- Auth       |
                  |    |-- IDOR       |
                  |    |-- SSRF       |
                  |    +-- Validator  |
                  +--------+----------+
                           |
              +------------+------------+
              |                         |
    +---------v---------+    +----------v---------+
    | vLLM Server       |    | PostgreSQL+pgvector|
    | Qwen2.5-72B-AWQ   |    | (persistence +     |
    | 2x A100 TP=2      |    |  semantic memory)  |
    | :8000              |    +--------------------+
    +-------------------+
```
