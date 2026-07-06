/**
 * @file extractor.go
 * @description Schema extraction implementation for drift detection
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/drift/extractor.go
 */

package drift

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// PostgreSQLSchemaExtractor extracts schema from PostgreSQL database
type PostgreSQLSchemaExtractor struct {
	db *sql.DB
}

// NewPostgreSQLSchemaExtractor creates a new PostgreSQL schema extractor
func NewPostgreSQLSchemaExtractor(db *sql.DB) *PostgreSQLSchemaExtractor {
	return &PostgreSQLSchemaExtractor{
		db: db,
	}
}

// ExtractSchema extracts complete database schema
func (e *PostgreSQLSchemaExtractor) ExtractSchema(ctx context.Context) (*SchemaSnapshot, error) {
	snapshot := &SchemaSnapshot{
		Version:     fmt.Sprintf("snapshot_%d", time.Now().Unix()),
		Timestamp:   time.Now(),
		Tables:      make(map[string]*TableSchema),
		Indexes:     make(map[string]*IndexSchema),
		Constraints: make(map[string]*ConstraintSchema),
	}

	// Get all tables
	tableNames, err := e.GetTableNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get table names: %w", err)
	}

	// Extract schema for each table
	for _, tableName := range tableNames {
		tableSchema, err := e.ExtractTableSchema(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to extract schema for table %s: %w", tableName, err)
		}
		snapshot.Tables[tableName] = tableSchema

		// Get indexes for table
		indexes, err := e.GetIndexes(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get indexes for table %s: %w", tableName, err)
		}
		for _, idx := range indexes {
			snapshot.Indexes[idx.Name] = &idx
		}

		// Get constraints for table
		constraints, err := e.GetConstraints(ctx, tableName)
		if err != nil {
			return nil, fmt.Errorf("failed to get constraints for table %s: %w", tableName, err)
		}
		for _, constraint := range constraints {
			snapshot.Constraints[constraint.Name] = &constraint
		}
	}

	return snapshot, nil
}

// ExtractTableSchema extracts schema for a specific table
func (e *PostgreSQLSchemaExtractor) ExtractTableSchema(ctx context.Context, tableName string) (*TableSchema, error) {
	tableSchema := &TableSchema{
		Name:        tableName,
		Columns:     make([]ColumnSchema, 0),
		Indexes:     make([]IndexSchema, 0),
		Constraints: make([]ConstraintSchema, 0),
	}

	// Query column information
	query := `
		SELECT
			column_name,
			data_type,
			is_nullable,
			column_default,
			character_maximum_length,
			numeric_precision,
			numeric_scale
		FROM information_schema.columns
		WHERE table_name = $1
		ORDER BY ordinal_position
	`

	rows, err := e.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var colName, dataType, isNullable string
		var defaultValue sql.NullString
		var maxLength, precision, scale sql.NullInt64

		err := rows.Scan(&colName, &dataType, &isNullable, &defaultValue, &maxLength, &precision, &scale)
		if err != nil {
			return nil, fmt.Errorf("failed to scan column: %w", err)
		}

		column := ColumnSchema{
			Name:       colName,
			DataType:   dataType,
			IsNullable: isNullable == "YES",
		}

		if defaultValue.Valid {
			column.DefaultValue = defaultValue.String
		}
		if maxLength.Valid {
			column.MaxLength = int(maxLength.Int64)
		}
		if precision.Valid {
			column.Precision = int(precision.Int64)
		}
		if scale.Valid {
			column.Scale = int(scale.Int64)
		}

		tableSchema.Columns = append(tableSchema.Columns, column)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating columns: %w", err)
	}

	// Get primary key
	pkQuery := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = $1::regclass
		AND i.indisprimary
	`

	pkRows, err := e.db.QueryContext(ctx, pkQuery, tableName)
	if err == nil {
		defer pkRows.Close()
		for pkRows.Next() {
			var colName string
			if err := pkRows.Scan(&colName); err == nil {
				tableSchema.PrimaryKey = append(tableSchema.PrimaryKey, colName)
			}
		}
	}

	return tableSchema, nil
}

// GetTableNames returns all table names in database
func (e *PostgreSQLSchemaExtractor) GetTableNames(ctx context.Context) ([]string, error) {
	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = 'public'
		AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := e.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	tableNames := make([]string, 0)
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tableNames = append(tableNames, tableName)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return tableNames, nil
}

// GetIndexes returns all indexes for a table
func (e *PostgreSQLSchemaExtractor) GetIndexes(ctx context.Context, tableName string) ([]IndexSchema, error) {
	query := `
		SELECT
			i.relname as index_name,
			a.attname as column_name,
			ix.indisunique as is_unique,
			am.amname as index_type
		FROM pg_class t
		JOIN pg_index ix ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		JOIN pg_am am ON i.relam = am.oid
		WHERE t.relname = $1
		AND ix.indisprimary = false
		ORDER BY i.relname, a.attnum
	`

	rows, err := e.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query indexes: %w", err)
	}
	defer rows.Close()

	indexMap := make(map[string]*IndexSchema)

	for rows.Next() {
		var indexName, columnName, indexType string
		var isUnique bool

		err := rows.Scan(&indexName, &columnName, &isUnique, &indexType)
		if err != nil {
			return nil, fmt.Errorf("failed to scan index: %w", err)
		}

		if idx, exists := indexMap[indexName]; exists {
			idx.Columns = append(idx.Columns, columnName)
		} else {
			indexMap[indexName] = &IndexSchema{
				Name:      indexName,
				TableName: tableName,
				Columns:   []string{columnName},
				Unique:    isUnique,
				Type:      indexType,
			}
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating indexes: %w", err)
	}

	indexes := make([]IndexSchema, 0, len(indexMap))
	for _, idx := range indexMap {
		indexes = append(indexes, *idx)
	}

	return indexes, nil
}

// GetConstraints returns all constraints for a table
func (e *PostgreSQLSchemaExtractor) GetConstraints(ctx context.Context, tableName string) ([]ConstraintSchema, error) {
	query := `
		SELECT
			tc.constraint_name,
			tc.constraint_type,
			kcu.column_name,
			ccu.table_name AS foreign_table_name,
			ccu.column_name AS foreign_column_name
		FROM information_schema.table_constraints AS tc
		LEFT JOIN information_schema.key_column_usage AS kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		LEFT JOIN information_schema.constraint_column_usage AS ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		WHERE tc.table_name = $1
		ORDER BY tc.constraint_name
	`

	rows, err := e.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query constraints: %w", err)
	}
	defer rows.Close()

	constraintMap := make(map[string]*ConstraintSchema)

	for rows.Next() {
		var constraintName, constraintType, columnName string
		var foreignTable, foreignColumn sql.NullString

		err := rows.Scan(&constraintName, &constraintType, &columnName, &foreignTable, &foreignColumn)
		if err != nil {
			return nil, fmt.Errorf("failed to scan constraint: %w", err)
		}

		if constraint, exists := constraintMap[constraintName]; exists {
			constraint.Columns = append(constraint.Columns, columnName)
			if foreignColumn.Valid {
				constraint.ReferencedColumns = append(constraint.ReferencedColumns, foreignColumn.String)
			}
		} else {
			constraint := &ConstraintSchema{
				Name:      constraintName,
				TableName: tableName,
				Type:      e.normalizeConstraintType(constraintType),
				Columns:   []string{columnName},
			}

			if foreignTable.Valid {
				constraint.ReferencedTable = foreignTable.String
			}
			if foreignColumn.Valid {
				constraint.ReferencedColumns = []string{foreignColumn.String}
			}

			constraintMap[constraintName] = constraint
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating constraints: %w", err)
	}

	constraints := make([]ConstraintSchema, 0, len(constraintMap))
	for _, constraint := range constraintMap {
		constraints = append(constraints, *constraint)
	}

	return constraints, nil
}

// normalizeConstraintType normalizes constraint type names
func (e *PostgreSQLSchemaExtractor) normalizeConstraintType(pgType string) string {
	switch pgType {
	case "PRIMARY KEY":
		return "primary_key"
	case "FOREIGN KEY":
		return "foreign_key"
	case "UNIQUE":
		return "unique"
	case "CHECK":
		return "check"
	default:
		return pgType
	}
}
