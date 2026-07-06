/**
 * @file extractor.go
 * @description Extract current database schema into declarative format
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/extractor.go
 */

package state

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PostgreSQLSchemaExtractor implements SchemaExtractor for PostgreSQL
type PostgreSQLSchemaExtractor struct {
	db     *sql.DB
	schema string // PostgreSQL schema name (default: public)
}

// NewPostgreSQLSchemaExtractor creates a new PostgreSQL schema extractor
func NewPostgreSQLSchemaExtractor(db *sql.DB) *PostgreSQLSchemaExtractor {
	return &PostgreSQLSchemaExtractor{
		db:     db,
		schema: "public",
	}
}

// ExtractSchema extracts complete current database schema
func (e *PostgreSQLSchemaExtractor) ExtractSchema(ctx context.Context) (*DatabaseSchema, error) {
	schema := &DatabaseSchema{
		Name:        e.schema,
		Version:     "current",
		Tables:      make([]Table, 0),
		Indexes:     make([]Index, 0),
		Constraints: make([]Constraint, 0),
		Triggers:    make([]Trigger, 0),
		Views:       make([]View, 0),
		Metadata:    make(map[string]interface{}),
	}

	// Get schema version if exists
	version, err := e.GetVersion(ctx)
	if err == nil && version != "" {
		schema.Version = version
	}

	// Extract tables
	tableNames, err := e.getTableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get table names: %w", err)
	}

	for _, tableName := range tableNames {
		table, err := e.ExtractTable(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to extract table %s: %w", tableName, err)
		}
		schema.Tables = append(schema.Tables, *table)
	}

	// Extract global views
	views, err := e.extractViews(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract views: %w", err)
	}
	schema.Views = views

	// Extract global triggers
	triggers, err := e.extractTriggers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to extract triggers: %w", err)
	}
	schema.Triggers = triggers

	// Add metadata
	schema.Metadata["extracted_at"] = time.Now().Format(time.RFC3339)
	schema.Metadata["database_type"] = "postgresql"

	return schema, nil
}

// ExtractTable extracts specific table schema
func (e *PostgreSQLSchemaExtractor) ExtractTable(ctx context.Context, tableName string) (*Table, error) {
	table := &Table{
		Name:        tableName,
		Columns:     make([]Column, 0),
		PrimaryKey:  make([]string, 0),
		Indexes:     make([]Index, 0),
		Constraints: make([]Constraint, 0),
		Options:     make(map[string]interface{}),
	}

	// Extract columns
	columns, err := e.extractColumns(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract columns: %w", err)
	}
	table.Columns = columns

	// Extract primary key
	pkColumns, err := e.extractPrimaryKey(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract primary key: %w", err)
	}
	table.PrimaryKey = pkColumns

	// Extract indexes
	indexes, err := e.extractTableIndexes(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract indexes: %w", err)
	}
	table.Indexes = indexes

	// Extract constraints
	constraints, err := e.extractTableConstraints(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to extract constraints: %w", err)
	}
	table.Constraints = constraints

	// Extract table comment
	comment, err := e.extractTableComment(ctx, tableName)
	if err == nil && comment != "" {
		table.Comment = comment
	}

	return table, nil
}

// GetVersion returns current schema version from database
func (e *PostgreSQLSchemaExtractor) GetVersion(ctx context.Context) (string, error) {
	// Check if schema_version table exists
	var exists bool
	err := e.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = 'schema_version'
		)
	`, e.schema).Scan(&exists)

	if err != nil || !exists {
		return "", nil
	}

	// Get latest version
	var version string
	err = e.db.QueryRowContext(ctx, `
		SELECT version FROM schema_version
		ORDER BY applied_at DESC LIMIT 1
	`).Scan(&version)

	if err == sql.ErrNoRows {
		return "", nil
	}

	return version, err
}

// SetVersion updates schema version in database
func (e *PostgreSQLSchemaExtractor) SetVersion(ctx context.Context, version string) error {
	// Create table if not exists
	_, err := e.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			applied_by VARCHAR(255),
			description TEXT
		)
	`)

	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Insert version
	_, err = e.db.ExecContext(ctx, `
		INSERT INTO schema_version (version, applied_at)
		VALUES ($1, CURRENT_TIMESTAMP)
	`, version)

	if err != nil {
		return fmt.Errorf("failed to insert version: %w", err)
	}

	return nil
}

// Private helper methods

func (e *PostgreSQLSchemaExtractor) getTableNames(ctx context.Context) ([]string, error) {
	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_type = 'BASE TABLE'
		  AND table_name NOT IN ('schema_version', 'goose_db_version', 'schema_migrations')
		ORDER BY table_name
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tableNames := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}

	return tableNames, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractColumns(ctx context.Context, tableName string) ([]Column, error) {
	query := `
		SELECT
			c.column_name,
			c.data_type,
			c.is_nullable,
			c.column_default,
			c.character_maximum_length,
			c.numeric_precision,
			c.numeric_scale,
			col_description((c.table_schema||'.'||c.table_name)::regclass::oid, c.ordinal_position) as column_comment
		FROM information_schema.columns c
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns := make([]Column, 0)
	for rows.Next() {
		var col Column
		var dataType, isNullable string
		var colDefault, comment sql.NullString
		var charMaxLen, numPrecision, numScale sql.NullInt64

		err := rows.Scan(
			&col.Name,
			&dataType,
			&isNullable,
			&colDefault,
			&charMaxLen,
			&numPrecision,
			&numScale,
			&comment,
		)
		if err != nil {
			return nil, err
		}

		col.Type = e.normalizeDataType(dataType)
		col.Nullable = (isNullable == "YES")

		if colDefault.Valid {
			col.Default = e.parseDefault(colDefault.String)
		}

		if charMaxLen.Valid {
			col.Length = int(charMaxLen.Int64)
		}

		if numPrecision.Valid {
			col.Precision = int(numPrecision.Int64)
		}

		if numScale.Valid {
			col.Scale = int(numScale.Int64)
		}

		if comment.Valid && comment.String != "" {
			col.Comment = comment.String
		}

		// Check if auto-increment (serial/identity)
		if colDefault.Valid {
			defaultStr := strings.ToLower(colDefault.String)
			if strings.Contains(defaultStr, "nextval") || strings.Contains(defaultStr, "identity") {
				col.AutoIncrement = true
				col.Default = nil // Don't include the sequence default
			}
		}

		columns = append(columns, col)
	}

	return columns, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractPrimaryKey(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		  AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pkColumns := make([]string, 0)
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		pkColumns = append(pkColumns, colName)
	}

	return pkColumns, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractTableIndexes(ctx context.Context, tableName string) ([]Index, error) {
	query := `
		SELECT
			i.relname as index_name,
			ix.indisunique as is_unique,
			am.amname as index_type,
			pg_get_indexdef(ix.indexrelid) as index_def
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_am am ON am.oid = i.relam
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1
		  AND t.relname = $2
		  AND ix.indisprimary = false
		ORDER BY i.relname
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := make([]Index, 0)
	for rows.Next() {
		var idx Index
		var isUnique bool
		var indexType, indexDef string

		if err := rows.Scan(&idx.Name, &isUnique, &indexType, &indexDef); err != nil {
			return nil, err
		}

		idx.Table = tableName
		idx.Unique = isUnique
		idx.Type = indexType

		// Parse columns from index definition
		idx.Columns = e.parseIndexColumns(indexDef)

		indexes = append(indexes, idx)
	}

	return indexes, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractTableConstraints(ctx context.Context, tableName string) ([]Constraint, error) {
	query := `
		SELECT
			con.conname as constraint_name,
			con.contype as constraint_type,
			pg_get_constraintdef(con.oid) as constraint_def
		FROM pg_constraint con
		JOIN pg_class t ON t.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		WHERE n.nspname = $1
		  AND t.relname = $2
		  AND con.contype IN ('f', 'u', 'c')
		ORDER BY con.conname
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	constraints := make([]Constraint, 0)
	for rows.Next() {
		var con Constraint
		var conType, conDef string

		if err := rows.Scan(&con.Name, &conType, &conDef); err != nil {
			return nil, err
		}

		con.Table = tableName

		// Parse constraint definition
		switch conType {
		case "f": // Foreign key
			con.Type = "foreign_key"
			e.parseForeignKey(&con, conDef)
		case "u": // Unique
			con.Type = "unique"
			con.Columns = e.parseConstraintColumns(conDef)
		case "c": // Check
			con.Type = "check"
			con.CheckExpression = e.parseCheckExpression(conDef)
			con.Columns = []string{} // Check constraints don't have specific columns
		}

		constraints = append(constraints, con)
	}

	return constraints, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractTableComment(ctx context.Context, tableName string) (string, error) {
	var comment sql.NullString
	err := e.db.QueryRowContext(ctx, `
		SELECT obj_description(($1 || '.' || $2)::regclass, 'pg_class')
	`, e.schema, tableName).Scan(&comment)

	if err != nil || !comment.Valid {
		return "", err
	}

	return comment.String, nil
}

func (e *PostgreSQLSchemaExtractor) extractViews(ctx context.Context) ([]View, error) {
	query := `
		SELECT
			v.table_name,
			v.view_definition,
			CASE WHEN mv.matviewname IS NOT NULL THEN true ELSE false END as is_materialized
		FROM information_schema.views v
		LEFT JOIN pg_matviews mv ON mv.matviewname = v.table_name AND mv.schemaname = $1
		WHERE v.table_schema = $1
		ORDER BY v.table_name
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := make([]View, 0)
	for rows.Next() {
		var view View
		var viewDef sql.NullString

		if err := rows.Scan(&view.Name, &viewDef, &view.Materialized); err != nil {
			return nil, err
		}

		if viewDef.Valid {
			view.Query = viewDef.String
		}

		views = append(views, view)
	}

	return views, rows.Err()
}

func (e *PostgreSQLSchemaExtractor) extractTriggers(ctx context.Context) ([]Trigger, error) {
	query := `
		SELECT
			t.tgname as trigger_name,
			c.relname as table_name,
			CASE t.tgtype::int & 2
				WHEN 2 THEN 'BEFORE'
				ELSE 'AFTER'
			END as trigger_when,
			CASE t.tgtype::int & 28
				WHEN 4 THEN 'INSERT'
				WHEN 8 THEN 'DELETE'
				WHEN 16 THEN 'UPDATE'
			END as trigger_event,
			pg_get_triggerdef(t.oid) as trigger_def
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1
		  AND NOT t.tgisinternal
		ORDER BY c.relname, t.tgname
	`

	rows, err := e.db.QueryContext(ctx, query, e.schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	triggers := make([]Trigger, 0)
	for rows.Next() {
		var trigger Trigger
		var triggerDef string

		if err := rows.Scan(&trigger.Name, &trigger.Table, &trigger.When, &trigger.Event, &triggerDef); err != nil {
			return nil, err
		}

		// Parse trigger body from definition
		trigger.Body = e.parseTriggerBody(triggerDef)
		trigger.ForEachRow = strings.Contains(strings.ToUpper(triggerDef), "FOR EACH ROW")

		triggers = append(triggers, trigger)
	}

	return triggers, rows.Err()
}

// Parsing helpers

func (e *PostgreSQLSchemaExtractor) normalizeDataType(pgType string) string {
	// Normalize PostgreSQL types to standard types
	typeMap := map[string]string{
		"character varying": "varchar",
		"character":         "char",
		"timestamp without time zone": "timestamp",
		"timestamp with time zone":    "timestamptz",
		"double precision":  "double",
		"int4":              "integer",
		"int8":              "bigint",
		"int2":              "smallint",
		"bool":              "boolean",
	}

	if normalized, exists := typeMap[pgType]; exists {
		return normalized
	}

	return pgType
}

func (e *PostgreSQLSchemaExtractor) parseDefault(defaultStr string) interface{} {
	defaultStr = strings.TrimSpace(defaultStr)

	// Handle NULL
	if strings.ToUpper(defaultStr) == "NULL" {
		return nil
	}

	// Handle booleans
	if defaultStr == "true" || defaultStr == "false" {
		return defaultStr == "true"
	}

	// Handle strings (quoted)
	if strings.HasPrefix(defaultStr, "'") && strings.HasSuffix(defaultStr, "'") {
		return strings.Trim(defaultStr, "'")
	}

	// Handle functions
	if strings.HasSuffix(strings.ToUpper(defaultStr), "()") {
		return defaultStr
	}

	// Return as-is
	return defaultStr
}

func (e *PostgreSQLSchemaExtractor) parseIndexColumns(indexDef string) []string {
	// Extract columns from CREATE INDEX ... ON table (col1, col2)
	start := strings.Index(indexDef, "(")
	end := strings.LastIndex(indexDef, ")")

	if start == -1 || end == -1 {
		return []string{}
	}

	colsPart := indexDef[start+1 : end]
	cols := strings.Split(colsPart, ",")

	result := make([]string, 0, len(cols))
	for _, col := range cols {
		col = strings.TrimSpace(col)
		// Remove any qualifiers (ASC, DESC, etc.)
		parts := strings.Fields(col)
		if len(parts) > 0 {
			result = append(result, parts[0])
		}
	}

	return result
}

func (e *PostgreSQLSchemaExtractor) parseConstraintColumns(conDef string) []string {
	// Extract columns from UNIQUE (col1, col2)
	start := strings.Index(conDef, "(")
	end := strings.LastIndex(conDef, ")")

	if start == -1 || end == -1 {
		return []string{}
	}

	colsPart := conDef[start+1 : end]
	cols := strings.Split(colsPart, ",")

	result := make([]string, 0, len(cols))
	for _, col := range cols {
		result = append(result, strings.TrimSpace(col))
	}

	return result
}

func (e *PostgreSQLSchemaExtractor) parseForeignKey(con *Constraint, conDef string) {
	// Parse: FOREIGN KEY (col1, col2) REFERENCES table(ref1, ref2) ON DELETE CASCADE

	// Extract local columns
	start := strings.Index(conDef, "(")
	end := strings.Index(conDef, ")")
	if start != -1 && end != -1 {
		colsPart := conDef[start+1 : end]
		con.Columns = strings.Split(strings.ReplaceAll(colsPart, " ", ""), ",")
	}

	// Extract referenced table and columns
	if idx := strings.Index(conDef, "REFERENCES "); idx != -1 {
		refPart := conDef[idx+11:]

		// Get table name. pg_get_constraintdef emits "REFERENCES users(id)"
		// with no space before the paren, so the first whitespace-delimited
		// token is "users(id)"; strip the column list to leave the bare table.
		parts := strings.Fields(refPart)
		if len(parts) > 0 {
			table := parts[0]
			if p := strings.Index(table, "("); p != -1 {
				table = table[:p]
			}
			con.ReferencedTable = table
		}

		// Get referenced columns
		start = strings.Index(refPart, "(")
		end = strings.Index(refPart, ")")
		if start != -1 && end != -1 {
			refCols := refPart[start+1 : end]
			con.ReferencedColumns = strings.Split(strings.ReplaceAll(refCols, " ", ""), ",")
		}
	}

	// Extract ON DELETE
	if idx := strings.Index(strings.ToUpper(conDef), "ON DELETE "); idx != -1 {
		rest := conDef[idx+10:]
		parts := strings.Fields(rest)
		if len(parts) > 0 {
			con.OnDelete = parts[0]
		}
	}

	// Extract ON UPDATE
	if idx := strings.Index(strings.ToUpper(conDef), "ON UPDATE "); idx != -1 {
		rest := conDef[idx+10:]
		parts := strings.Fields(rest)
		if len(parts) > 0 {
			con.OnUpdate = parts[0]
		}
	}
}

func (e *PostgreSQLSchemaExtractor) parseCheckExpression(conDef string) string {
	// Extract expression from CHECK (expression)
	start := strings.Index(conDef, "(")
	end := strings.LastIndex(conDef, ")")

	if start == -1 || end == -1 {
		return conDef
	}

	return conDef[start+1 : end]
}

func (e *PostgreSQLSchemaExtractor) parseTriggerBody(triggerDef string) string {
	// Extract body from trigger definition
	// This is simplified - actual parsing would be more complex
	if idx := strings.Index(strings.ToUpper(triggerDef), "EXECUTE "); idx != -1 {
		return triggerDef[idx+8:]
	}

	return triggerDef
}
