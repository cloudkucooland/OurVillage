# OurVillage Player

Run the human-controlled TUI with an existing client configuration:

```sh
go run ./cmd/player --config player.yaml
```

The config must provide the character's MQTT username, password, and broker:

```yaml
client:
  agent_id: "your_character_id"
  password: "your_character_password"
  broker_url: "tcp://localhost:1883"
```

If `--config` is omitted, the command looks for `agent.yaml` or `agent.yml` in
the current directory. `AGENTMUD_CONFIG` can also specify the config path.
Inside the TUI, `/help` lists the direct action commands. Plain text is not
sent to or interpreted by an LLM.
