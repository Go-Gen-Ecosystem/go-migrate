/**
 * @file generator.go
 * @description Auto-generates SQL migrations from schema differences
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/generator.go
 */

package state

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMigrationGenerator implements MigrationGenerator interface
type DefaultMigrationGenerator struct {
	dialect string
}

// NewDefaultMigrationGenerator creates a new migration generator
func NewDefaultMigrationGenerator(dialect string) *DefaultMigrationGenerator {
	if dialect == "" {
		dialect = "postgresql" // default
	}

	return &DefaultMigrationGenerator{
		dialect: dialect,
	}
}

// Generate creates migration SQL from schema diff
func (g *DefaultMigrationGenerator) Generate(ctx context.Context, diff *SchemaDiff, desired *DatabaseSchema) (*GeneratedMigration, error) {
	if diff == nil {
		return nil, fmt.Errorf("diff cannot be nil")
	}

	if !diff.HasChanges {
		return nil, fmt.Errorf("no changes to generate migration for")
	}

	// Generate UP SQL
	upSQL, err := g.GenerateUp(ctx, diff, desired)
	if err != nil {
		return nil, fmt.Errorf("failed to generate UP migration: %w", err)
	}

	// Generate DOWN SQL
	downSQL, err := g.GenerateDown(ctx, diff, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate DOWN migration: %w", err)
	}

	// Create migration
	migration := &GeneratedMigration{
		ID:              g.generateMigrationID(),
		Version:         desired.Version,
		Description:     g.generateDescription(diff),
		UpSQL:           upSQL,
		DownSQL:         downSQL,
		Diff:            diff,
		GeneratedAt:     time.Now(),
		SafeToAutoApply: diff.Summary.SafeToApply,
		Warnings:        g.generateWarnings(diff),
	}

	return migration, nil
}

// GenerateUp generates only UP migration SQL
func (g *DefaultMigrationGenerator) GenerateUp(ctx context.Context, diff *SchemaDiff, desired *DatabaseSchema) (string, error) {
	var sql strings.Builder

	sql.WriteString("-- Auto-generated migration (UP)\n")
	sql.WriteString(fmt.Sprintf("-- Generated at: %s\n", time.Now().Format(time.RFC3339)))
	sql.WriteString(fmt.Sprintf("-- Dialect: %s\n\n", g.dialect))

	// 1. Drop views (they depend on tables)
	if len(diff.ViewsDropped) > 0 {
		sql.WriteString("-- Drop views\n")
		for _, view := range diff.ViewsDropped {
			sql.WriteString(g.generateDropView(&view))
		}
		sql.WriteString("\n")
	}

	// 2. Drop triggers
	if len(diff.TriggersDropped) > 0 {
		sql.WriteString("-- Drop triggers\n")
		for _, trigger := range diff.TriggersDropped {
			sql.WriteString(g.generateDropTrigger(&trigger))
		}
		sql.WriteString("\n")
	}

	// 3. Drop constraints (before modifying tables)
	if len(diff.ConstraintsDropped) > 0 {
		sql.WriteString("-- Drop constraints\n")
		for _, constraint := range diff.ConstraintsDropped {
			sql.WriteString(g.generateDropConstraint(&constraint))
		}
		sql.WriteString("\n")
	}

	// 4. Drop indexes
	if len(diff.IndexesDropped) > 0 {
		sql.WriteString("-- Drop indexes\n")
		for _, index := range diff.IndexesDropped {
			sql.WriteString(g.generateDropIndex(&index))
		}
		sql.WriteString("\n")
	}

	// 5. Create new tables
	if len(diff.TablesAdded) > 0 {
		sql.WriteString("-- Create tables\n")
		for _, table := range diff.TablesAdded {
			sql.WriteString(g.generateCreateTable(&table))
		}
		sql.WriteString("\n")
	}

	// 6. Modify existing tables
	if len(diff.TablesModified) > 0 {
		sql.WriteString("-- Modify tables\n")
		for _, tableDiff := range diff.TablesModified {
			sql.WriteString(g.generateAlterTable(&tableDiff))
		}
		sql.WriteString("\n")
	}

	// 7. Drop tables
	if len(diff.TablesDropped) > 0 {
		sql.WriteString("-- Drop tables\n")
		for _, table := range diff.TablesDropped {
			sql.WriteString(g.generateDropTable(&table))
		}
		sql.WriteString("\n")
	}

	// 8. Create indexes
	if len(diff.IndexesAdded) > 0 {
		sql.WriteString("-- Create indexes\n")
		for _, index := range diff.IndexesAdded {
			sql.WriteString(g.generateCreateIndex(&index))
		}
		sql.WriteString("\n")
	}

	// 9. Create constraints
	if len(diff.ConstraintsAdded) > 0 {
		sql.WriteString("-- Create constraints\n")
		for _, constraint := range diff.ConstraintsAdded {
			sql.WriteString(g.generateAddConstraint(&constraint))
		}
		sql.WriteString("\n")
	}

	// 10. Create triggers
	if len(diff.TriggersAdded) > 0 {
		sql.WriteString("-- Create triggers\n")
		for _, trigger := range diff.TriggersAdded {
			sql.WriteString(g.generateCreateTrigger(&trigger))
		}
		sql.WriteString("\n")
	}

	// 11. Create views
	if len(diff.ViewsAdded) > 0 {
		sql.WriteString("-- Create views\n")
		for _, view := range diff.ViewsAdded {
			sql.WriteString(g.generateCreateView(&view))
		}
		sql.WriteString("\n")
	}

	return sql.String(), nil
}

// GenerateDown generates only DOWN migration SQL
func (g *DefaultMigrationGenerator) GenerateDown(ctx context.Context, diff *SchemaDiff, actual *DatabaseSchema) (string, error) {
	var sql strings.Builder

	sql.WriteString("-- Auto-generated migration (DOWN)\n")
	sql.WriteString(fmt.Sprintf("-- Generated at: %s\n", time.Now().Format(time.RFC3339)))
	sql.WriteString(fmt.Sprintf("-- Dialect: %s\n\n", g.dialect))

	// Reverse order of UP migration

	// 1. Drop views
	if len(diff.ViewsAdded) > 0 {
		sql.WriteString("-- Drop views\n")
		for _, view := range diff.ViewsAdded {
			sql.WriteString(g.generateDropView(&view))
		}
		sql.WriteString("\n")
	}

	// 2. Drop triggers
	if len(diff.TriggersAdded) > 0 {
		sql.WriteString("-- Drop triggers\n")
		for _, trigger := range diff.TriggersAdded {
			sql.WriteString(g.generateDropTrigger(&trigger))
		}
		sql.WriteString("\n")
	}

	// 3. Drop constraints
	if len(diff.ConstraintsAdded) > 0 {
		sql.WriteString("-- Drop constraints\n")
		for _, constraint := range diff.ConstraintsAdded {
			sql.WriteString(g.generateDropConstraint(&constraint))
		}
		sql.WriteString("\n")
	}

	// 4. Drop indexes
	if len(diff.IndexesAdded) > 0 {
		sql.WriteString("-- Drop indexes\n")
		for _, index := range diff.IndexesAdded {
			sql.WriteString(g.generateDropIndex(&index))
		}
		sql.WriteString("\n")
	}

	// 5. Drop tables that were added
	if len(diff.TablesAdded) > 0 {
		sql.WriteString("-- Drop tables\n")
		for _, table := range diff.TablesAdded {
			sql.WriteString(g.generateDropTable(&table))
		}
		sql.WriteString("\n")
	}

	// 6. Recreate tables that were dropped
	if len(diff.TablesDropped) > 0 {
		sql.WriteString("-- Recreate tables\n")
		for _, table := range diff.TablesDropped {
			sql.WriteString(g.generateCreateTable(&table))
		}
		sql.WriteString("\n")
	}

	// Note: Reversing column modifications is complex and may not be possible
	// without data loss, so we add a comment
	if len(diff.TablesModified) > 0 {
		sql.WriteString("-- WARNING: Reversing column modifications may cause data loss\n")
		sql.WriteString("-- Manual intervention may be required\n\n")
	}

	return sql.String(), nil
}

// GenerateFile writes migration to file in standard format
func (g *DefaultMigrationGenerator) GenerateFile(ctx context.Context, migration *GeneratedMigration, outputDir string) (string, error) {
	if migration == nil {
		return "", fmt.Errorf("migration cannot be nil")
	}

	if outputDir == "" {
		outputDir = "migrations"
	}

	// Ensure directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate filename: YYYYMMDDHHMMSS_description.sql
	timestamp := migration.GeneratedAt.Format("20060102150405")
	description := g.sanitizeFilename(migration.Description)
	filename := fmt.Sprintf("%s_%s.sql", timestamp, description)
	filepath := filepath.Join(outputDir, filename)

	// Create file content
	var content strings.Builder
	content.WriteString(fmt.Sprintf("-- Migration: %s\n", migration.Description))
	content.WriteString(fmt.Sprintf("-- Generated: %s\n", migration.GeneratedAt.Format(time.RFC3339)))
	content.WriteString(fmt.Sprintf("-- Version: %s\n", migration.Version))
	content.WriteString(fmt.Sprintf("-- Safe to auto-apply: %t\n\n", migration.SafeToAutoApply))

	if len(migration.Warnings) > 0 {
		content.WriteString("-- WARNINGS:\n")
		for _, warning := range migration.Warnings {
			content.WriteString(fmt.Sprintf("-- * %s\n", warning))
		}
		content.WriteString("\n")
	}

	content.WriteString("-- +goose Up\n")
	content.WriteString(migration.UpSQL)
	content.WriteString("\n-- +goose Down\n")
	content.WriteString(migration.DownSQL)

	// Write to file
	if err := os.WriteFile(filepath, []byte(content.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write migration file: %w", err)
	}

	return filepath, nil
}

// SetDialect configures SQL dialect for generation
func (g *DefaultMigrationGenerator) SetDialect(dialect string) {
	g.dialect = dialect
}

// SQL generation helpers

func (g *DefaultMigrationGenerator) generateCreateTable(table *Table) string {
	var sql strings.Builder

	sql.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", g.quoteIdentifier(table.Name)))

	// Columns
	for i, col := range table.Columns {
		sql.WriteString("  ")
		sql.WriteString(g.generateColumnDefinition(&col))
		if i < len(table.Columns)-1 || len(table.PrimaryKey) > 0 {
			sql.WriteString(",")
		}
		sql.WriteString("\n")
	}

	// Primary key
	if len(table.PrimaryKey) > 0 {
		quotedCols := make([]string, len(table.PrimaryKey))
		for i, col := range table.PrimaryKey {
			quotedCols[i] = g.quoteIdentifier(col)
		}
		sql.WriteString(fmt.Sprintf("  PRIMARY KEY (%s)\n", strings.Join(quotedCols, ", ")))
	}

	sql.WriteString(");\n")

	// Table indexes
	for _, idx := range table.Indexes {
		idx.Table = table.Name
		sql.WriteString(g.generateCreateIndex(&idx))
	}

	// Table constraints
	for _, con := range table.Constraints {
		con.Table = table.Name
		sql.WriteString(g.generateAddConstraint(&con))
	}

	return sql.String()
}

func (g *DefaultMigrationGenerator) generateColumnDefinition(col *Column) string {
	var def strings.Builder

	def.WriteString(g.quoteIdentifier(col.Name))
	def.WriteString(" ")
	def.WriteString(g.formatColumnType(col))

	if !col.Nullable {
		def.WriteString(" NOT NULL")
	}

	if col.Default != nil {
		def.WriteString(fmt.Sprintf(" DEFAULT %s", g.formatDefault(col.Default)))
	}

	if col.AutoIncrement {
		if g.dialect == "postgresql" {
			// PostgreSQL uses SERIAL or IDENTITY
			def.WriteString(" GENERATED BY DEFAULT AS IDENTITY")
		} else if g.dialect == "mysql" {
			def.WriteString(" AUTO_INCREMENT")
		}
	}

	if col.Unique {
		def.WriteString(" UNIQUE")
	}

	return def.String()
}

func (g *DefaultMigrationGenerator) formatColumnType(col *Column) string {
	baseType := col.Type

	if col.Length > 0 {
		return fmt.Sprintf("%s(%d)", baseType, col.Length)
	}

	if col.Precision > 0 {
		if col.Scale > 0 {
			return fmt.Sprintf("%s(%d,%d)", baseType, col.Precision, col.Scale)
		}
		return fmt.Sprintf("%s(%d)", baseType, col.Precision)
	}

	return baseType
}

func (g *DefaultMigrationGenerator) formatDefault(value interface{}) string {
	if value == nil {
		return "NULL"
	}

	switch v := value.(type) {
	case string:
		// Check if it's a function call
		if strings.HasSuffix(strings.ToUpper(v), "()") {
			return v
		}
		return fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
	case bool:
		if g.dialect == "postgresql" {
			if v {
				return "true"
			}
			return "false"
		}
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (g *DefaultMigrationGenerator) generateDropTable(table *Table) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;\n", g.quoteIdentifier(table.Name))
}

func (g *DefaultMigrationGenerator) generateAlterTable(tableDiff *TableDiff) string {
	var sql strings.Builder

	tableName := g.quoteIdentifier(tableDiff.TableName)

	// Add columns
	for _, col := range tableDiff.ColumnsAdded {
		sql.WriteString(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;\n",
			tableName, g.generateColumnDefinition(&col)))
	}

	// Drop columns
	for _, col := range tableDiff.ColumnsDropped {
		sql.WriteString(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;\n",
			tableName, g.quoteIdentifier(col.Name)))
	}

	// Modify columns
	for _, colDiff := range tableDiff.ColumnsModified {
		sql.WriteString(fmt.Sprintf("-- Modify column %s.%s\n", tableDiff.TableName, colDiff.ColumnName))
		sql.WriteString(fmt.Sprintf("-- Changes: %s\n", strings.Join(colDiff.Changes, ", ")))
		sql.WriteString("-- TODO: Add specific ALTER COLUMN statements\n")
	}

	return sql.String()
}

func (g *DefaultMigrationGenerator) generateCreateIndex(idx *Index) string {
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}

	quotedCols := make([]string, len(idx.Columns))
	for i, col := range idx.Columns {
		quotedCols[i] = g.quoteIdentifier(col)
	}

	sql := fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
		unique,
		g.quoteIdentifier(idx.Name),
		g.quoteIdentifier(idx.Table),
		strings.Join(quotedCols, ", "))

	if idx.Where != "" {
		sql += fmt.Sprintf(" WHERE %s", idx.Where)
	}

	return sql + ";\n"
}

func (g *DefaultMigrationGenerator) generateDropIndex(idx *Index) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s;\n", g.quoteIdentifier(idx.Name))
}

func (g *DefaultMigrationGenerator) generateAddConstraint(con *Constraint) string {
	tableName := g.quoteIdentifier(con.Table)
	constraintName := g.quoteIdentifier(con.Name)

	var constraintDef string

	switch strings.ToUpper(con.Type) {
	case "FOREIGN_KEY":
		quotedCols := make([]string, len(con.Columns))
		for i, col := range con.Columns {
			quotedCols[i] = g.quoteIdentifier(col)
		}

		quotedRefCols := make([]string, len(con.ReferencedColumns))
		for i, col := range con.ReferencedColumns {
			quotedRefCols[i] = g.quoteIdentifier(col)
		}

		constraintDef = fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(quotedCols, ", "),
			g.quoteIdentifier(con.ReferencedTable),
			strings.Join(quotedRefCols, ", "))

		if con.OnDelete != "" {
			constraintDef += fmt.Sprintf(" ON DELETE %s", con.OnDelete)
		}
		if con.OnUpdate != "" {
			constraintDef += fmt.Sprintf(" ON UPDATE %s", con.OnUpdate)
		}

	case "UNIQUE":
		quotedCols := make([]string, len(con.Columns))
		for i, col := range con.Columns {
			quotedCols[i] = g.quoteIdentifier(col)
		}
		constraintDef = fmt.Sprintf("UNIQUE (%s)", strings.Join(quotedCols, ", "))

	case "CHECK":
		constraintDef = fmt.Sprintf("CHECK (%s)", con.CheckExpression)

	default:
		return fmt.Sprintf("-- Unsupported constraint type: %s\n", con.Type)
	}

	return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s;\n",
		tableName, constraintName, constraintDef)
}

func (g *DefaultMigrationGenerator) generateDropConstraint(con *Constraint) string {
	return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s;\n",
		g.quoteIdentifier(con.Table), g.quoteIdentifier(con.Name))
}

func (g *DefaultMigrationGenerator) generateCreateTrigger(trigger *Trigger) string {
	return fmt.Sprintf("-- TODO: Create trigger %s on %s\n-- Trigger creation is database-specific\n",
		trigger.Name, trigger.Table)
}

func (g *DefaultMigrationGenerator) generateDropTrigger(trigger *Trigger) string {
	return fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s;\n",
		g.quoteIdentifier(trigger.Name), g.quoteIdentifier(trigger.Table))
}

func (g *DefaultMigrationGenerator) generateCreateView(view *View) string {
	materialized := ""
	if view.Materialized {
		materialized = "MATERIALIZED "
	}

	return fmt.Sprintf("CREATE %sVIEW %s AS\n%s;\n",
		materialized, g.quoteIdentifier(view.Name), view.Query)
}

func (g *DefaultMigrationGenerator) generateDropView(view *View) string {
	materialized := ""
	if view.Materialized {
		materialized = "MATERIALIZED "
	}

	return fmt.Sprintf("DROP %sVIEW IF EXISTS %s;\n",
		materialized, g.quoteIdentifier(view.Name))
}

func (g *DefaultMigrationGenerator) quoteIdentifier(name string) string {
	if g.dialect == "mysql" {
		return fmt.Sprintf("`%s`", name)
	}
	// PostgreSQL, SQLite
	return fmt.Sprintf("\"%s\"", name)
}

func (g *DefaultMigrationGenerator) generateMigrationID() string {
	return fmt.Sprintf("M%s", time.Now().Format("20060102150405"))
}

func (g *DefaultMigrationGenerator) generateDescription(diff *SchemaDiff) string {
	parts := make([]string, 0)

	if len(diff.TablesAdded) > 0 {
		parts = append(parts, fmt.Sprintf("add %d table(s)", len(diff.TablesAdded)))
	}
	if len(diff.TablesDropped) > 0 {
		parts = append(parts, fmt.Sprintf("drop %d table(s)", len(diff.TablesDropped)))
	}
	if len(diff.TablesModified) > 0 {
		parts = append(parts, fmt.Sprintf("modify %d table(s)", len(diff.TablesModified)))
	}

	if len(parts) == 0 {
		return "schema_changes"
	}

	return strings.Join(parts, "_")
}

func (g *DefaultMigrationGenerator) generateWarnings(diff *SchemaDiff) []string {
	warnings := make([]string, 0)

	if len(diff.TablesDropped) > 0 {
		warnings = append(warnings, fmt.Sprintf("Dropping %d table(s) - potential data loss", len(diff.TablesDropped)))
	}

	for _, tableDiff := range diff.TablesModified {
		if len(tableDiff.ColumnsDropped) > 0 {
			warnings = append(warnings, fmt.Sprintf("Dropping %d column(s) from %s - potential data loss",
				len(tableDiff.ColumnsDropped), tableDiff.TableName))
		}
	}

	if !diff.Summary.SafeToApply {
		warnings = append(warnings, "Migration contains destructive changes - manual review recommended")
	}

	return warnings
}

func (g *DefaultMigrationGenerator) sanitizeFilename(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	s = strings.ReplaceAll(s, "-", "_")

	// Remove non-alphanumeric characters
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			result.WriteRune(r)
		}
	}

	// Limit length
	if result.Len() > 50 {
		return result.String()[:50]
	}

	return result.String()
}
