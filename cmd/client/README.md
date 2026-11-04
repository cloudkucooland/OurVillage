# AgentMUD Client (`agentmud-client`)

`agentmud-client` is the autonomous agent daemon ("Agent Mind") for the **AgentMUD (VillageOS)** persistent economic simulation. While the server deterministically governs physical reality, spatial presence, inventories, and transactional validation over SQLite and Mochi-MQTT, each `agentmud-client` process powers an individual character's cognitive lifecycle, dialogue generation, and autonomous decision-making.

---

## 1. Architectural Philosophy & Hardware Heterogeneity

In AgentMUD, agents are not ephemeral scripts running in a centralized monolith; they are designed as independent, distributed processes that can run across diverse physical and virtual environments:

*   **Low-Power Edge Hardware (Initial Focus: Raspberry Pi 4 & 5)**: Local ARM64 single-board computers hosting autonomous village residents on bare metal.
*   **Neural Accelerator SBCs (Rockchip RK3588)**: Edge devices taking advantage of dedicated on-chip NPUs via RKLLM.
*   **Workstations & Dedicated Servers**: x86_64 or Apple Silicon machines capable of hosting multiple agent clients or higher-parameter models.
*   **Cloud & Remote Model APIs**: Hosted endpoints (OpenAI, Google Gemini, Anthropic Claude, OpenRouter, or self-hosted Ollama/vLLM servers).

To accommodate this variety without tying the codebase to any single inference engine, `agentmud-client` enforces a strict abstraction between the **Agent Harness** (MQTT event handling, context window assembly, intent dispatch) and the **Inference Engine** (model evaluation).

---

## 2. Model Layer Abstraction

### Pluggable Inference Engine Interface

The model layer exposes a unified Go interface:

```go
package model

import (
    "context"
)

// GenerationOptions parameterizes inference requests.
type GenerationOptions struct {
    Temperature   float32
    TopP          float32
    MaxTokens     int
    StopSequences []string
    Grammar       string // Optional GBNF grammar or JSON schema constraint
}

// InferenceEngine defines the contract for all local and cloud model providers.
type InferenceEngine interface {
    // Name returns the provider identifier (e.g. "llama-pure", "cloud-openai").
    Name() string

    // Generate generates a complete text response given a structured prompt.
    Generate(ctx context.Context, prompt string, opts GenerationOptions) (string, error)

    // Stream generates tokens asynchronously over a channel.
    Stream(ctx context.Context, prompt string, opts GenerationOptions, onToken func(token string) bool) error

    // Close releases model weights, dynamic libraries, or network connections.
    Close() error
}
```

---

## 3. Supported Model Backends

### 3.1 Local Embedded Inference via `go-llama-pure` (Raspberry Pi & Edge Devices)

For local execution on edge hardware such as Raspberry Pi and standard Linux/macOS/Windows systems, `agentmud-client` utilizes [**`github.com/develerltd/go-llama-pure`**](https://github.com/develerltd/go-llama-pure).

#### Key Advantages
*   **Pure Go Compilation (`CGO_ENABLED=0`)**: Leverages [`ebitengine/purego`](https://github.com/ebitengine/purego) to resolve and bind symbols from the dynamic `libllama.so` library at runtime.
*   **Seamless Cross-Compilation**: The `agentmud-client` binary can be compiled on x86_64 developer machines for `linux/arm64` without installing a complex C cross-compilation toolchain.
*   **Quantized GGUF Support**: Directly loads modern quantized formats (Q4_K_M, Q3_K_M, IQ4_XS) optimized for low memory footprints.
*   **Target Small Language Models (SLMs)**:
    *   *Qwen 2.5 (0.5B, 1.5B, 3B)*
    *   *SmolLM2 (1.7B)*
    *   *Llama 3.2 (1B, 3B)*
    *   *Phi-3.5 Mini (3.8B)*

#### Host Setup Requirements
While compilation requires no C toolchain, host devices running `llama-pure` must have `libllama.so` present:
```bash
# On Raspberry Pi (Debian/Raspberry Pi OS 64-bit):
# Build or install prebuilt llama.cpp shared library:
git clone https://github.com/ggml-org/llama.cpp
cd llama.cpp
cmake -B build -DBUILD_SHARED_LIBS=ON
cmake --build build --config Release -j$(nproc)
sudo cp build/bin/libllama.so /usr/local/lib/
sudo ldconfig
```

### 3.2 Rockchip NPU (`rkllm`)
*   Designed for Rockchip RK3588/RK3588S SBCs (e.g., Orange Pi 5, Radxa Rock 5).
*   Interfaces with the RKLLM runtime (`librkllm_api.so`) to utilize the 6 TOPS NPU, offloading all computation from the CPU.

### 3.3 Cloud & Remote APIs
*   **OpenAI-Compatible (`cloud-openai`)**: Generic REST driver supporting OpenAI (`gpt-4o-mini`), OpenRouter, Groq, Together AI, or remote Ollama/vLLM servers (`http://host:11434/v1`).
*   **Google Gemini (`cloud-gemini`)**: Native REST/gRPC client for Google Gemini models (`gemini-1.5-flash`, `gemini-2.5-flash`).
*   **Anthropic Claude (`cloud-anthropic`)**: Direct integration for Claude 3.5 Haiku / Sonnet.
*   **Mock / Headless Engine (`mock`)**: Deterministic stub engine returning canned JSON intents for CI/CD unit testing and integration tests without network or weight overhead.

---

## 4. Startup Configuration & Provider Selection

The active model provider and client parameters are determined dynamically at startup using a layered configuration hierarchy:
**CLI Flags > Environment Variables > Config File (`agent.yaml`) > Built-in Defaults**.

### CLI Flags
```bash
# Example 1: Run on Raspberry Pi with local GGUF model via go-llama-pure
./agentmud-client \
  --broker="tcp://192.168.1.50:1883" \
  --agent="eldon_millwright" \
  --pass="secret_eldon_123" \
  --engine="llama-pure" \
  --model-path="/opt/models/qwen2.5-1.5b-instruct-q4_k_m.gguf" \
  --llama-lib="/usr/local/lib/libllama.so" \
  --threads=4 \
  --context-size=2048

# Example 2: Run with cloud model via OpenAI-compatible endpoint
./agentmud-client \
  --broker="tcp://localhost:1883" \
  --agent="martha_barkeep" \
  --engine="cloud-openai" \
  --api-key="${OPENAI_API_KEY}" \
  --api-base="https://api.openai.com/v1" \
  --model-name="gpt-4o-mini"

# Example 3: Run using configuration file
./agentmud-client --config="/etc/agentmud/eldon.yaml"
```

### Configuration File (`agent.yaml`)
```yaml
# AgentMUD Client Configuration
client:
  agent_id: "eldon_millwright"
  password: "secret_eldon_123"
  broker_url: "tcp://192.168.1.50:1883"
  client_id: "" # Auto-generated if empty
  keepalive_seconds: 60

# Model Provider Specification
model:
  # Options: "llama-pure", "rkllm", "cloud-openai", "cloud-gemini", "cloud-anthropic", "mock"
  provider: "llama-pure"
  
  # Parameters for "llama-pure"
  llama_pure:
    library_path: "/usr/local/lib/libllama.so"
    model_path: "/opt/models/qwen2.5-1.5b-instruct-q4_k_m.gguf"
    context_size: 2048
    threads: 4
    gpu_layers: 0           # Set > 0 if compiled with Vulkan/OpenCL on Pi
    temperature: 0.7
    top_p: 0.9
    repeat_penalty: 1.15
    grammar_file: "config/grammars/agent_intent.gbnf"

  # Parameters for cloud providers (if provider is cloud-*)
  cloud:
    api_base: "https://api.openai.com/v1"
    api_key: ""             # Reads from AGENTMUD_API_KEY if blank
    model_name: "gpt-4o-mini"
    timeout_seconds: 15
    max_tokens: 256

# Autonomous Cognitive Settings
cognition:
  tick_interval_debounce_ms: 2000
  chat_response_probability: 0.85
  max_sensory_log_lines: 12
  reflection_interval_ticks: 30
```

---

## 5. Constraints & Operational Realities (Raspberry Pi & Edge Hardware)

Operating LLM agents on resource-constrained devices like the Raspberry Pi introduces distinct engineering challenges that the client harness must handle:

### 5.1 Memory Budgeting (RAM)
*   A Raspberry Pi 4/5 with 4GB or 8GB of RAM must share memory between the Linux OS, system daemons, the `agentmud-client` runtime, and the model weights.
*   **Recommended Model Size**: Quantized 1.5B models (e.g. Qwen 2.5 1.5B Q4_K_M) require ~1.1 GB of RAM; 3B models require ~2.0 GB. Models exceeding 3B parameters should be avoided on 4GB Pi devices to prevent swap thrashing and OOM kills.
*   Model weights and KV caches are pinned upon startup and must not be re-allocated during runtime ticks.

### 5.2 Inference Latency & Asynchronous MQTT Loop
*   Inference on a Pi 4/5 CPU generates roughly 6–18 tokens/second depending on quantization and thread tuning. A typical 80-token structured response takes 4–12 seconds.
*   **Non-Blocking Event Queue**: The MQTT connection and message ingestion must **never** block on inference. Incoming time ticks, location whispers, and chat lines must be buffered into a local concurrent ring buffer while the cognitive worker evaluates a decision.
*   **Debounce & Action Cooldown**: If multiple chat messages arrive during an active inference generation, they are collected into the next context assembly cycle rather than spawning concurrent inference jobs.

### 5.3 Thermal & Core Management
*   Sustained multi-threaded matrix operations produce elevated CPU temperatures.
*   The client should default to `threads = nproc - 1` (e.g., 3 threads on a 4-core Pi 4/5) to maintain operating system responsiveness, handle network I/O, and mitigate thermal throttling under passive cooling.

### 5.4 Grammar-Constrained Sampling & Output Resilience
*   Smaller models (0.5B to 3B) are prone to hallucinating markdown commentary, omitting required JSON braces, or outputting invalid action names.
*   **GBNF Grammars**: When using `go-llama-pure`, inference can be constrained via a GBNF (GGML BNF) grammar file to enforce valid JSON matching the RFC 002 Intent schema.
*   **Sanitization Fallback**: The client includes a regex/JSON repair step to extract valid JSON blocks from responses and validate them before submitting an intent to the server.

---

## 6. Client Features & Development Roadmap

```
+---------------------------------------------------------------------------------+
|                               agentmud-client                                   |
|                                                                                 |
|  +---------------------+   +-----------------------+   +---------------------+  |
|  |     MQTT Engine     |   | Context Window Mgr    |   |  Cognitive Loop &   |  |
|  | - Auto-reconnect    |   | - Profile & Needs     |   |  Decision Engine    |  |
|  | - Topic ACL sub/unsub|  | - Room Sensory Stream |   | - Reactive Triggers |  |
|  | - Dispatcher        |   | - Episodic Memories   |   | - Periodic Ticks    |  |
|  +----------+----------+   +-----------+-----------+   +----------+----------+  |
|             |                          |                          |             |
|             +--------------------------+--------------------------+             |
|                                        |                                        |
|                         +--------------v--------------+                         |
|                         |    Inference Engine Layer   |                         |
|                         +--------------+--------------+                         |
|                                        |                                        |
|         +-------------------+----------+----------+-------------------+         |
|         |                   |                     |                   |         |
|  +------v------+     +------v------+       +------v------+     +------v------+  |
|  |go-llama-pure|     |    RKLLM    |       |Cloud OpenAI |     | Mock Engine |  |
|  |(RPi / Edge) |     |  (RK3588)   |       |(API / Remote|     |  (CI / Test)|  |
|  +-------------+     +-------------+       +-------------+     +-------------+  |
+---------------------------------------------------------------------------------+
```

### Phase 1: Core Harness & Abstraction Layer
- [x] Define the `model.InferenceEngine` interface and options struct.
- [x] Build the configuration parser supporting CLI flags, environment variables, and `agent.yaml`.
- [x] Implement the `mock` engine for automated test suites.

### Phase 2: Inference Provider Implementations
- [x] Integrate `develerltd/go-llama-pure` dynamic loader (`libllama.so` binding, model context initialization, prompt generation).
- [x] Implement `cloud-openai` HTTP REST client supporting OpenAI, OpenRouter, and Ollama endpoints.
- [x] Implement GBNF grammar parser to constrain model outputs to RFC 002 JSON schemas.

### Phase 3: Spatial Context Window Manager
- [x] Implement sliding sensory ring buffer for incoming room chat, whispers, and system events.
- [x] Dynamic location topic subscriptions: automatically unsubscribe from old room topics and subscribe to new room topics upon `MOVE` intent confirmation.
- [x] Ingest character profile, hunger/energy thresholds, and inventory states from `world/agents/{agent_id}/direct`.
- [x] Inject pairwise relationship sentiments and episodic memories (RFC 013).

### Phase 4: Intent Dispatcher & Error Recovery
- [x] Intent validator verifying action enum (`MOVE`, `CHAT`, `TRANSFER`, `FORAGE`, `PICKUP`, `DROP`, `EQUIP`, `UNEQUIP`).
- [x] Retry/repair loop for malformed JSON before dispatching to `world/agents/{agent_id}/intent`.
- [x] Correlation tracker matching dispatched `intent_id` with incoming server direct execution results.

### Phase 5: Raspberry Pi Deployment & Tooling
- [ ] Terminal UI / status dashboard showing live agent state, recent sensory inputs, thought processes, and tokens/sec inference metrics.
