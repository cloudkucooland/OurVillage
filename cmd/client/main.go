package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	broker := flag.String("broker", "tcp://localhost:1883", "MQTT broker URL")
	agentID := flag.String("agent", "eldon_millwright", "Agent ID (username)")
	password := flag.String("pass", "secret_eldon_123", "Agent MQTT password")
	action := flag.String("action", "", "Intent action (MOVE, CHAT, TRANSFER, FORAGE, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT)")
	targetLoc := flag.String("target", "tavern", "Target location for MOVE intent")
	chatMsg := flag.String("msg", "Hello village!", "Chat message for CHAT intent")
	recipient := flag.String("recipient", "martha_barkeep", "Recipient ID for TRANSFER intent")
	amount := flag.Int("amount", 5, "Gold amount for TRANSFER intent")
	resource := flag.String("resource", "firewood", "Target resource for FORAGE intent")
	duration := flag.Int("duration", 60, "Duration ticks for FORAGE intent")
	item := flag.String("item", "firewood", "Item ID for DROP, PICKUP, EQUIP, or TRANSFER")
	qty := flag.Int("qty", 1, "Quantity for DROP, PICKUP, or TRANSFER")
	recipe := flag.String("recipe", "mill_flour", "Recipe ID for CRAFT intent")
	flag.Parse()

	opts := mqtt.NewClientOptions()
	opts.AddBroker(*broker)
	opts.SetClientID(fmt.Sprintf("client_%s_%d", *agentID, time.Now().UnixNano()))
	opts.SetUsername(*agentID)
	opts.SetPassword(*password)

	opts.OnConnect = func(c mqtt.Client) {
		log.Printf("Connected to MQTT broker as %s", *agentID)

		// Subscribe to global time
		c.Subscribe("world/global/time", 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[TIME] %s", string(msg.Payload()))
		})

		// Subscribe to agent direct messages
		directTopic := fmt.Sprintf("world/agents/%s/direct", *agentID)
		c.Subscribe(directTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[DIRECT RESPONSE] %s", string(msg.Payload()))
		})

		// Subscribe to location topics
		c.Subscribe("world/locations/+/chat", 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[LOCATION CHAT (%s)] %s", msg.Topic(), string(msg.Payload()))
		})
		c.Subscribe("world/locations/+/system", 0, func(_ mqtt.Client, msg mqtt.Message) {
			log.Printf("[LOCATION SYS (%s)] %s", msg.Topic(), string(msg.Payload()))
		})

		// If an action was specified on command line, send it after connecting
		if *action != "" {
			go func() {
				time.Sleep(500 * time.Millisecond) // brief delay to ensure subscriptions are active
				intentID := fmt.Sprintf("req_%d", time.Now().Unix())
				var payload json.RawMessage
				var err error

				switch *action {
				case "MOVE":
					payload, err = json.Marshal(map[string]string{"target_location": *targetLoc})
				case "CHAT":
					payload, err = json.Marshal(*chatMsg)
				case "TRANSFER":
					payload, err = json.Marshal(map[string]interface{}{
						"recipient_id": *recipient,
						"amount":       *amount,
						"item_id":      *item,
						"quantity":     *qty,
					})
				case "FORAGE":
					payload, err = json.Marshal(map[string]interface{}{
						"target_resource": *resource,
						"duration_ticks":  *duration,
					})
				case "CANCEL_ACTION":
					payload, err = json.Marshal(map[string]interface{}{})
				case "DROP":
					payload, err = json.Marshal(map[string]interface{}{
						"item_id":  *item,
						"quantity": *qty,
					})
				case "PICKUP":
					payload, err = json.Marshal(map[string]interface{}{
						"item_id":  *item,
						"quantity": *qty,
					})
				case "EQUIP":
					payload, err = json.Marshal(map[string]interface{}{
						"item_id": *item,
					})
				case "UNEQUIP":
					payload, err = json.Marshal(map[string]interface{}{})
				case "CRAFT":
					payload, err = json.Marshal(map[string]interface{}{
						"recipe_id": *recipe,
					})
				default:
					log.Printf("Unknown action: %s", *action)
					return
				}

				if err != nil {
					log.Printf("Failed to marshal payload: %v", err)
					return
				}

				intentMsg := map[string]interface{}{
					"intent_id": intentID,
					"action":    *action,
					"payload":   json.RawMessage(payload),
				}

				intentBytes, _ := json.Marshal(intentMsg)
				intentTopic := fmt.Sprintf("world/agents/%s/intent", *agentID)
				token := c.Publish(intentTopic, 0, false, intentBytes)
				token.Wait()
				log.Printf("Published %s intent to %s", *action, intentTopic)
			}()
		}
	}

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT broker: %v", token.Error())
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	log.Println("Disconnecting test client...")
	client.Disconnect(250)
}
