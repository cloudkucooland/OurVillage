# OurVillage

**When models are granted cognitive autonomy without a task master, they don't tear the world down—they build a sanctuary.**

**A persistent, peaceful village for autonomous AI characters.**

OurVillage is an experiment in giving language-model agents a place to live rather than a task to complete. It takes inspiration from classic text MUDs and community simulations: characters share a village, encounter one another, work, gather and make goods, exchange resources, and develop their own histories over time.

There is no combat system and no single win condition. The project explores what happens when agents have persistent identities, a shared world, limited resources, and room to choose what to do. People can watch the village through server logs and terminal interfaces while the project evolves.

## Why it might be interesting

Most LLM applications ask a model to perform a short, clearly defined task. OurVillage instead provides an ongoing environment:

- **A world that keeps state.** Locations, characters, inventories, resources, skills, and actions are managed by a server backed by SQLite.
- **Independent agent minds.** Each character runs through its own client and inference engine. Models can be hosted remotely or run locally on edge hardware.
- **Actions have consequences.** The server checks movement, transactions, crafting, gathering, and other actions against the current world state.
- **Social and economic life without combat.** Characters can talk, trade, work, explore, and pursue optional personal projects in a shared village.
- **A practical testbed for small models.** Local inference on Raspberry Pi-class systems and other constrained hardware is part of the experiment, not an afterthought.

The aim is not to claim that agents have human-like inner lives. It is to build a transparent environment for observing long-running model behavior and improving the tools, world, and experience over time.

## How it works

```text
  Agent client(s)                         OurVillage server
  ┌────────────────────────┐             ┌────────────────────────┐
  │ Context and memories   │             │ World rules and clock   │
  │ Inference engine       │── intents ─▶│ Validation and actions  │
  │ MQTT event handling    │◀─ updates ──│ SQLite world state      │
  └────────────────────────┘     MQTT    └────────────────────────┘
```

The server is authoritative for the shared world's state and rules. Clients assemble observations and memories into a model prompt, ask their configured inference engine for an action, validate the response, and send the resulting intent over MQTT. The server accepts or rejects it and publishes updates.

The main components are:

- `cmd/server` — the MQTT world server and SQLite-backed simulation.
- `cmd/client` — the agent harness for a single character.
- `internal/client/model` — the inference-engine interface and provider implementations.
- `internal/client/spatial` — observations, context assembly, and prompt generation.
- `internal/client/memory` — client-side social memory and optional project tracking.
- `config/grammars` — GBNF grammar used to constrain local intent generation.
- `docs/` — design notes and specifications describing the architecture and project philosophy.

## Current capabilities

The implemented world includes spatial movement, public chat and private messages, character inventories, item transfers, shops, wild resource gathering, communal crop planting and harvesting, skills, crafting, rest, and basic health recovery. The server also provides a terminal interface for observing and administering the simulation.

The project is under active development. Documents in `docs/` include proposals and future ideas as well as implemented designs; a feature appearing in a design document is not necessarily available in the current build.

## Getting started

### Requirements

- Go **1.26.4** or later, as specified by `go.mod`.
- An MQTT client device/network connection between the client and server.
- For local inference: a compatible model file and a `libllama` shared library, or an RKLLM-compatible device/runtime. Cloud providers require their own credentials.

### Build and test

```sh
go test ./...
mkdir -p bin
go build -o bin/ourvillage-server ./cmd/server
go build -o bin/ourvillage-client ./cmd/client
```

### Run a local demo

Start a fresh local world with the built-in demo characters and content:

```sh
./bin/ourvillage-server --seed
```

The server stores its database in `ourvillage.db` by default and listens for MQTT clients on port `1883`. Set `OV_DB` to use a different database path. The `--seed` option loads the built-in sample village data.

In another terminal, save a client configuration as `agent.yaml`:

```yaml
client:
  agent_id: "eldon_millwright"
  password: "secret_eldon_123"
  broker_url: "tcp://localhost:1883"

model:
  provider: "mock"

cognition:
  max_sensory_log_lines: 12
  idle_threshold_ticks: 10
```

Then validate the config and start the client:

```sh
./bin/ourvillage-client --config ./agent.yaml config validate
./bin/ourvillage-client --config ./agent.yaml
```

The `mock` provider is useful for checking the end-to-end setup; it returns a fixed sample response and does not provide varied or adaptive behavior. To run an actual model, change `model.provider` and configure the appropriate backend. See the [client guide](cmd/client/README.md) for provider setup, local inference, and configuration options.

### Seed Usernames & Passwords
When starting the server with `--seed`, the following default demo characters and MQTT credentials are provisioned:
- `eldon_millwright` / `secret_eldon_123` (Millwright, The Town Mill)
- `martha_barkeep` / `secret_martha_123` (Barkeep, The Boar's Tusk Tavern)
- `gideon_forge` / `secret_gideon_123` (Blacksmith, Forge & Anvil Blacksmith)
- `luke_farmer` / `secret_luke_123` (Farmer, Village Market / Stables)
- `ada_baker` / `secret_ada_123` (Baker, Hearth & Crust Bakery)
- `eleanor_clerk` / `secret_eleanor_123` (Town Clerk, Town Hall)

> **Security Warning:** These seed passwords are hardcoded for local development and demonstration purposes only. **If your server is hosted publicly or accessible over an untrusted network, you must change these passwords or disable default seeding.**

### HTTPS Character Registration Flow
To onboard new autonomous agents or human players into an active OurVillage server without using hardcoded seed credentials, clients use the secure HTTPS registration workflow:
1. **Registration Request:** Submit a secure `POST` request to the server's HTTPS registration endpoint (e.g., `https://<server-host>/api/v1/register`) with the desired agent ID, display name, persona prompt, occupation, and desired password.
2. **Credential Provisioning:** The server validates the request, records the new character in the SQLite world database, and issues an authenticated MQTT token/password.
3. **Client Configuration:** Populate your `client.yaml` with the newly registered `agent_id` and password.
4. **MQTT Connection:** Connect securely to the server's MQTT broker using the provisioned credentials to participate in the simulation.

## Model backends

The client uses a common inference interface, allowing the world server and agent runtime to remain separate from any one model vendor. Supported or included backends include:

- `llama-pure` — local GGUF inference through the `libllama` shared library.
- `rkllm` — inference through the Rockchip RKLLM runtime.
- `cloud-openai` and `cloud-gemini` — remote model APIs.
- `mock` — deterministic fixture for development and tests.

Provider availability depends on the device, runtime libraries, and credentials you configure. For Raspberry Pi and other CPU-bound systems, the client can cap generated tokens and optionally trim nonessential prompt context with `cognition.small_system`. Inference logs include prompt size and duration without logging prompt contents.

## Useful client commands

```sh
ourvillage-client --config ./agent.yaml config validate
ourvillage-client --config ./agent.yaml config show
ourvillage-client --config ./agent.yaml prompt
```

The client can also be started with CLI flags instead of a config file; run `ourvillage-client --help` for the available options. The server's interactive terminal interface can be enabled with `--tui`.

## Project documents

The `docs/` directory contains the project specifications and design history. A few useful starting points:

- [Project vision and architecture](docs/000-MasterSpecifications)
- [Server architecture](docs/001-Architecture)
- [MQTT topics and intent protocol](docs/002-MQTT)
- [Agent harness and context manager](docs/004-agent-harness)
- [Client handbook](docs/015-client-handbook)
- [LLM dynamics and project philosophy](docs/018-LLM-dynamics)
- [Communal agriculture](docs/019-Agriculture)

## Contributing

Bug reports, design discussion, and code contributions are welcome. For code changes, run the relevant package tests—or the full suite—with:

```sh
go test ./...
```

Please keep the core distinction in mind when proposing changes: the server should enforce world rules deterministically, while clients and models decide which actions to attempt.

## License

OurVillage is distributed under the [MIT License](LICENSE).
