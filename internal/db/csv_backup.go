package db

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// csvBackupTables are exported in dependency order. Import clears them in
// reverse order and restores them in this order.
var csvBackupTables = []string{
	"locations",
	"items",
	"characters",
	"location_connections",
	"location_operators",
	"location_items",
	"location_resources",
	"location_shop_items",
	"workstations",
	"recipes",
	"recipe_ingredients",
	"notice_boards",
	"contracts",
	"character_skills",
	"character_known_recipes",
	"inventories",
	"farm_crops",
	"farm_plots",
	"event_logs",
	"server_state",
}

const csvNull = `\N`

// ExportCSV writes a consistent database snapshot as one CSV file per table.
func ExportCSV(database *sql.DB, directory string) error {
	if database == nil {
		return fmt.Errorf("database is not connected")
	}
	if strings.TrimSpace(directory) == "" {
		return fmt.Errorf("backup directory is required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin backup snapshot: %w", err)
	}
	defer tx.Rollback()
	for _, table := range csvBackupTables {
		if err := exportCSVTable(tx, directory, table); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("finish backup snapshot: %w", err)
	}
	return nil
}

func exportCSVTable(tx *sql.Tx, directory, table string) error {
	rows, err := tx.Query("SELECT * FROM " + table)
	if err != nil {
		return fmt.Errorf("export table %s: %w", table, err)
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return fmt.Errorf("read columns for %s: %w", table, err)
	}
	file, err := os.CreateTemp(directory, "."+table+"-*.csv")
	if err != nil {
		rows.Close()
		return fmt.Errorf("create CSV for %s: %w", table, err)
	}
	tempPath := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(tempPath)
	}
	writer := csv.NewWriter(file)
	if err := writer.Write(columns); err != nil {
		rows.Close()
		cleanup()
		return fmt.Errorf("write CSV header for %s: %w", table, err)
	}
	values := make([]any, len(columns))
	valuePointers := make([]any, len(columns))
	for i := range values {
		valuePointers[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(valuePointers...); err != nil {
			rows.Close()
			cleanup()
			return fmt.Errorf("read row from %s: %w", table, err)
		}
		record := make([]string, len(values))
		for i, value := range values {
			record[i] = encodeCSVValue(value)
		}
		if err := writer.Write(record); err != nil {
			rows.Close()
			cleanup()
			return fmt.Errorf("write row to %s: %w", table, err)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		cleanup()
		return fmt.Errorf("read table %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close table %s: %w", table, err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		cleanup()
		return fmt.Errorf("flush CSV for %s: %w", table, err)
	}
	if err := file.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync CSV for %s: %w", table, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("close CSV for %s: %w", table, err)
	}
	if err := os.Rename(tempPath, filepath.Join(directory, table+".csv")); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("publish CSV for %s: %w", table, err)
	}
	return nil
}

// ImportCSV completely replaces the persisted world state from a CSV backup.
// The import runs in one transaction, so a malformed backup leaves the
// existing database unchanged.
func ImportCSV(database *sql.DB, directory string) error {
	if database == nil {
		return fmt.Errorf("database is not connected")
	}
	if strings.TrimSpace(directory) == "" {
		return fmt.Errorf("backup directory is required")
	}
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin restore transaction: %w", err)
	}
	defer tx.Rollback()
	for i := len(csvBackupTables) - 1; i >= 0; i-- {
		if _, err := tx.Exec("DELETE FROM " + csvBackupTables[i]); err != nil {
			return fmt.Errorf("clear table %s: %w", csvBackupTables[i], err)
		}
	}
	for _, table := range csvBackupTables {
		if err := importCSVTable(tx, directory, table); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit restore: %w", err)
	}
	return nil
}

func importCSVTable(tx *sql.Tx, directory, table string) error {
	file, err := os.Open(filepath.Join(directory, table+".csv"))
	if err != nil {
		return fmt.Errorf("open %s.csv: %w", table, err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read header from %s.csv: %w", table, err)
	}
	columnsRows, err := tx.Query("SELECT * FROM " + table + " LIMIT 0")
	if err != nil {
		return fmt.Errorf("inspect table %s: %w", table, err)
	}
	columns, err := columnsRows.Columns()
	closeErr := columnsRows.Close()
	if err != nil {
		return fmt.Errorf("read columns for %s: %w", table, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close column query for %s: %w", table, closeErr)
	}
	if !equalStrings(header, columns) {
		return fmt.Errorf("%s.csv columns do not match this database schema", table)
	}
	placeholders := make([]string, len(columns))
	quotedColumns := make([]string, len(columns))
	for i, column := range columns {
		placeholders[i] = "?"
		quotedColumns[i] = `"` + strings.ReplaceAll(column, `"`, `""`) + `"`
	}
	statement := "INSERT INTO " + table + " (" + strings.Join(quotedColumns, ",") + ") VALUES (" + strings.Join(placeholders, ",") + ")"
	stmt, err := tx.Prepare(statement)
	if err != nil {
		return fmt.Errorf("prepare restore for %s: %w", table, err)
	}
	defer stmt.Close()
	line := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			return fmt.Errorf("read %s.csv line %d: %w", table, line, err)
		}
		if len(record) != len(columns) {
			return fmt.Errorf("%s.csv line %d has %d fields, expected %d", table, line, len(record), len(columns))
		}
		values := make([]any, len(record))
		for i, value := range record {
			values[i] = decodeCSVValue(value)
		}
		if _, err := stmt.Exec(values...); err != nil {
			return fmt.Errorf("restore %s.csv line %d: %w", table, line, err)
		}
	}
	return nil
}

func encodeCSVValue(value any) string {
	if value == nil {
		return csvNull
	}
	var text string
	switch value := value.(type) {
	case []byte:
		text = string(value)
	case string:
		text = value
	default:
		text = fmt.Sprint(value)
	}
	if strings.HasPrefix(text, `\`) {
		return `\` + text
	}
	return text
}

func decodeCSVValue(value string) any {
	if value == csvNull {
		return nil
	}
	if strings.HasPrefix(value, `\\`) {
		return value[1:]
	}
	return value
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
