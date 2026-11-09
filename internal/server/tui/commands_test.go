package tui

import (
	"database/sql"
	"strings"
	"testing"

	"agentmud/internal/db"
	"agentmud/internal/server/admin"
)

func TestParseCommandLine(t *testing.T) {
	args, err := parseCommandLine(`create-item apple "Red Apple" 0.25 "A crisp \"fresh\" fruit"`)
	if err != nil {
		t.Fatalf("parseCommandLine returned error: %v", err)
	}
	want := []string{"create-item", "apple", "Red Apple", "0.25", `A crisp "fresh" fruit`}
	if len(args) != len(want) {
		t.Fatalf("got %d args, want %d: %#v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, args[i], want[i])
		}
	}

	for _, input := range []string{`create-item apple "unfinished`, `create-item apple\`} {
		if _, err := parseCommandLine(input); err == nil {
			t.Errorf("expected parse error for %q", input)
		}
	}
}

func TestCreateCharacterPasswordIsNotStoredInCommandHistory(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	model := NewModel(database, admin.NewAdminCommandExecutor(database, nil, nil), nil)
	model.state = adminTab
	model = model.executeCommand(`create-character test_agent "Test Agent" Tester town_square "A test persona."`)

	if model.sensitiveOutput == "" {
		t.Fatal("expected generated password to be displayed separately")
	}
	if strings.Contains(strings.Join(model.history, "\n"), model.sensitiveOutput) {
		t.Fatal("generated password was stored in command history")
	}
	if !strings.Contains(strings.Join(model.history, "\n"), "Created character test_agent") {
		t.Fatal("expected successful creation to be recorded")
	}
	var name string
	if err := database.QueryRow("SELECT name FROM characters WHERE id = 'test_agent'").Scan(&name); err != nil || name != "Test Agent" {
		t.Errorf("quoted character name was not preserved: name=%q err=%v", name, err)
	}

	model = model.executeCommand("help")
	if model.sensitiveOutput != "" {
		t.Error("generated password should be cleared after the next command")
	}
}

func TestCreateItemCommandProperties(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	model := NewModel(database, admin.NewAdminCommandExecutor(database, nil, nil), nil)
	model = model.executeCommand(`create-item bread "Fresh Bread" 0.3 "A warm loaf." --consumable 25`)
	model = model.executeCommand(`create-item basket "Woven Basket" 0.5 "A carrying basket." --container 30 8`)

	for _, test := range []struct {
		id            string
		consumable    int
		hungerRestore int
		container     int
		capacity      float64
		slots         int
	}{
		{id: "bread", consumable: 1, hungerRestore: 25},
		{id: "basket", container: 1, capacity: 30, slots: 8},
	} {
		var consumable, hungerRestore, container, slots int
		var capacity float64
		err := database.QueryRow(`
			SELECT is_consumable, hunger_restore, is_container, capacity_kg, slot_limit
			FROM items WHERE id = ?
		`, test.id).Scan(&consumable, &hungerRestore, &container, &capacity, &slots)
		if err != nil {
			t.Errorf("failed to query %s: %v", test.id, err)
			continue
		}
		if consumable != test.consumable || hungerRestore != test.hungerRestore ||
			container != test.container || capacity != test.capacity || slots != test.slots {
			t.Errorf("%s properties = %d, %d, %d, %.1f, %d", test.id, consumable, hungerRestore, container, capacity, slots)
		}
	}

	model = model.executeCommand(`create-item invalid "Invalid Food" 0.3 "Bad properties." --consumable nope`)
	if !strings.Contains(strings.Join(model.history, "\n"), "hunger_restore must be an integer") {
		t.Error("expected invalid hunger restore to produce a command error")
	}
	var invalidCount int
	if err := database.QueryRow("SELECT COUNT(*) FROM items WHERE id = 'invalid'").Scan(&invalidCount); err != nil || invalidCount != 0 {
		t.Errorf("invalid item should not be created: count=%d err=%v", invalidCount, err)
	}
}

func TestConfigureResourceAndShopCommands(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	model := NewModel(database, admin.NewAdminCommandExecutor(database, nil, nil), nil)
	model = model.executeCommand("set-resource prairie wild_grain 25 80 10")
	model = model.executeCommand("set-shop-item bakery bread 2 14")

	var stock, maxStock, replenish int
	if err := database.QueryRow(`
		SELECT current_stock, max_stock, replenish_rate_per_day FROM location_resources
		WHERE location_id = 'prairie' AND resource_id = 'wild_grain'
	`).Scan(&stock, &maxStock, &replenish); err != nil {
		t.Fatalf("failed to read resource config: %v", err)
	}
	if stock != 25 || maxStock != 80 || replenish != 10 {
		t.Errorf("unexpected resource config: %d/%d replenish %d", stock, maxStock, replenish)
	}
	var price, quantity int
	if err := database.QueryRow(`
		SELECT price_gold, stock_quantity FROM location_shop_items
		WHERE location_id = 'bakery' AND item_id = 'bread'
	`).Scan(&price, &quantity); err != nil {
		t.Fatalf("failed to read shop config: %v", err)
	}
	if price != 2 || quantity != 14 {
		t.Errorf("unexpected shop config: %d gold, stock %d", price, quantity)
	}

	model = model.executeCommand("set-resource prairie wild_grain 10 nope 2")
	if !strings.Contains(strings.Join(model.history, "\n"), "stock and replenish values must be integers") {
		t.Error("expected invalid resource values to produce a command error")
	}
	model = model.executeCommand("set-shop-item bakery bread -3 10")
	if !strings.Contains(strings.Join(model.history, "\n"), "price must be >= 0") {
		t.Error("expected invalid shop price to produce a command error")
	}
}

func TestSetLocationParameterCommands(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	model := NewModel(database, admin.NewAdminCommandExecutor(database, nil, nil), nil)
	model = model.executeCommand("set-public tavern public")
	model = model.executeCommand("set-indoor tavern outdoor")
	model = model.executeCommand("set-access tavern foot_only")
	model = model.executeCommand("set-hours tavern 20:00 04:00")
	model = model.executeCommand(`set-rules tavern "Quiet after closing."`)
	model = model.executeCommand("set-owner tavern none")

	var isPublic, isIndoor bool
	var accessTier, openTime, closeTime, rules string
	if err := database.QueryRow(`
		SELECT is_public_property, is_indoor, vehicle_access_tier, open_time, close_time, rules_prompt
		FROM locations WHERE id = 'tavern'
	`).Scan(&isPublic, &isIndoor, &accessTier, &openTime, &closeTime, &rules); err != nil {
		t.Fatalf("failed to read location settings: %v", err)
	}
	if !isPublic || isIndoor || accessTier != "FOOT_ONLY" ||
		openTime != "20:00" || closeTime != "04:00" || rules != "Quiet after closing." {
		t.Errorf("unexpected location settings: public=%v indoor=%v access=%s hours=%s-%s rules=%q",
			isPublic, isIndoor, accessTier, openTime, closeTime, rules)
	}
	var owner sql.NullString
	if err := database.QueryRow("SELECT owner_id FROM locations WHERE id = 'tavern'").Scan(&owner); err != nil {
		t.Fatalf("failed to read owner: %v", err)
	}
	if owner.Valid {
		t.Errorf("expected owner to be cleared, got %q", owner.String)
	}

	model = model.executeCommand("set-access tavern no_vehicles")
	if !strings.Contains(strings.Join(model.history, "\n"), "invalid access tier") {
		t.Error("expected invalid access tier to be reported")
	}
	model = model.executeCommand("set-hours tavern 24:00 04:00")
	if !strings.Contains(strings.Join(model.history, "\n"), "invalid opening time") {
		t.Error("expected invalid opening time to be reported")
	}
}

func TestFullHealCommandRestoresHealthAndHunger(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`
		UPDATE characters
		SET health = 0, hunger = 0, status = 'RESTING',
			action_target = 'town_mill', action_start_tick = 10, action_end_tick = 70,
			action_intent_id = ''
		WHERE id = 'eldon_millwright'
	`); err != nil {
		t.Fatal(err)
	}

	model := NewModel(database, admin.NewAdminCommandExecutor(database, nil, nil), nil)
	model = model.executeCommand("full-heal eldon_millwright")

	var health, hunger, startTick, endTick int
	var status, target, intentID string
	if err := database.QueryRow(`
		SELECT health, hunger, status, action_target, action_start_tick, action_end_tick, action_intent_id
		FROM characters WHERE id = 'eldon_millwright'
	`).Scan(&health, &hunger, &status, &target, &startTick, &endTick, &intentID); err != nil {
		t.Fatal(err)
	}
	if health != 100 || hunger != 100 || status != "IDLE" ||
		target != "" || startTick != 0 || endTick != 0 || intentID != "" {
		t.Errorf("full-heal state = health %d, hunger %d, status %q, action %q/%d-%d/%q",
			health, hunger, status, target, startTick, endTick, intentID)
	}
	if !strings.Contains(strings.Join(model.history, "\n"), "Restored health and hunger for eldon_millwright") {
		t.Errorf("expected full-heal command success, got %q", strings.Join(model.history, "\n"))
	}

	model = model.executeCommand("full-heal missing_character")
	if !strings.Contains(strings.Join(model.history, "\n"), "not found") {
		t.Errorf("expected unknown character to be rejected, got %q", strings.Join(model.history, "\n"))
	}
}

func TestWeatherCommandSupportsNewWeatherTypes(t *testing.T) {
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	defer database.Close()

	weather := ""
	model := NewModel(database, admin.NewAdminCommandExecutor(database, func(value string) {
		weather = value
	}, nil), nil)
	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "weather fog", want: "fog"},
		{input: "weather snow", want: "snow"},
		{input: "weather cloudy", want: "cloudy"},
		{input: "weather hail", want: "hail"},
		{input: "weather windy", want: "windy"},
		{input: "weather heatwave", want: "heatwave"},
		{input: "weather drought", want: "drought"},
		{input: "weather frost", want: "frost"},
	} {
		model = model.executeCommand(test.input)
		if weather != test.want {
			t.Errorf("%s set weather to %q, want %q", test.input, weather, test.want)
		}
	}

	model.state = adminTab
	if view := model.View(); !strings.Contains(view, "drought|fog|frost|hail|heatwave") || !strings.Contains(view, "storm|windy") {
		t.Errorf("admin help/autocomplete missing added weather options:\n%s", view)
	}
}
