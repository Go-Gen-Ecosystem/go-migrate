/**
 * @file differ.go
 * @description Schema differ implementation - compares desired vs actual schema
 * @author Claude Code
 * @date 2025-01-01
 * @path pkg/core/database/migrations/internal/components/state/differ.go
 */

package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// DefaultSchemaDiffer implements SchemaDiffer interface
type DefaultSchemaDiffer struct {
	config *StateBasedConfig
}

// NewDefaultSchemaDiffer creates a new schema differ
func NewDefaultSchemaDiffer(config *StateBasedConfig) *DefaultSchemaDiffer {
	if config == nil {
		config = &StateBasedConfig{
			AllowDestructive: false,
		}
	}

	return &DefaultSchemaDiffer{
		config: config,
	}
}

// Diff compares desired schema with actual schema
func (d *DefaultSchemaDiffer) Diff(ctx context.Context, desired *DatabaseSchema, actual *DatabaseSchema) (*SchemaDiff, error) {
	if desired == nil {
		return nil, fmt.Errorf("desired schema cannot be nil")
	}

	if actual == nil {
		return nil, fmt.Errorf("actual schema cannot be nil")
	}

	diff := &SchemaDiff{
		HasChanges:         false,
		TablesAdded:        make([]Table, 0),
		TablesDropped:      make([]Table, 0),
		TablesModified:     make([]TableDiff, 0),
		IndexesAdded:       make([]Index, 0),
		IndexesDropped:     make([]Index, 0),
		ConstraintsAdded:   make([]Constraint, 0),
		ConstraintsDropped: make([]Constraint, 0),
		TriggersAdded:      make([]Trigger, 0),
		TriggersDropped:    make([]Trigger, 0),
		ViewsAdded:         make([]View, 0),
		ViewsDropped:       make([]View, 0),
	}

	// Build table maps for easier comparison
	desiredTables := make(map[string]*Table)
	for i := range desired.Tables {
		desiredTables[desired.Tables[i].Name] = &desired.Tables[i]
	}

	actualTables := make(map[string]*Table)
	for i := range actual.Tables {
		actualTables[actual.Tables[i].Name] = &actual.Tables[i]
	}

	// Find added and modified tables
	for name, desiredTable := range desiredTables {
		if actualTable, exists := actualTables[name]; exists {
			// Table exists - check for modifications
			tableDiff, err := d.DiffTables(ctx, desiredTable, actualTable)
			if err != nil {
				return nil, fmt.Errorf("failed to diff table %s: %w", name, err)
			}

			if d.hasTableChanges(tableDiff) {
				diff.TablesModified = append(diff.TablesModified, *tableDiff)
				diff.HasChanges = true
			}
		} else {
			// Table doesn't exist - needs to be added
			diff.TablesAdded = append(diff.TablesAdded, *desiredTable)
			diff.HasChanges = true
		}
	}

	// Find dropped tables
	for name, actualTable := range actualTables {
		if _, exists := desiredTables[name]; !exists {
			diff.TablesDropped = append(diff.TablesDropped, *actualTable)
			diff.HasChanges = true
		}
	}

	// Diff global indexes
	d.diffIndexes(desired.Indexes, actual.Indexes, diff)

	// Diff global constraints
	d.diffConstraints(desired.Constraints, actual.Constraints, diff)

	// Diff triggers
	d.diffTriggers(desired.Triggers, actual.Triggers, diff)

	// Diff views
	d.diffViews(desired.Views, actual.Views, diff)

	// Calculate summary
	diff.Summary = d.calculateSummary(diff)

	// Determine safety
	diff.Summary.SafeToApply = d.CalculateSafety(ctx, diff)

	return diff, nil
}

// DiffTables compares specific tables
func (d *DefaultSchemaDiffer) DiffTables(ctx context.Context, desired *Table, actual *Table) (*TableDiff, error) {
	if desired == nil || actual == nil {
		return nil, fmt.Errorf("tables cannot be nil")
	}

	tableDiff := &TableDiff{
		TableName:       desired.Name,
		ColumnsAdded:    make([]Column, 0),
		ColumnsDropped:  make([]Column, 0),
		ColumnsModified: make([]ColumnDiff, 0),
	}

	// Build column maps
	desiredCols := make(map[string]*Column)
	for i := range desired.Columns {
		desiredCols[desired.Columns[i].Name] = &desired.Columns[i]
	}

	actualCols := make(map[string]*Column)
	for i := range actual.Columns {
		actualCols[actual.Columns[i].Name] = &actual.Columns[i]
	}

	// Find added and modified columns
	for name, desiredCol := range desiredCols {
		if actualCol, exists := actualCols[name]; exists {
			// Column exists - check for modifications
			if colDiff := d.diffColumns(desiredCol, actualCol); colDiff != nil {
				tableDiff.ColumnsModified = append(tableDiff.ColumnsModified, *colDiff)
			}
		} else {
			// Column doesn't exist - needs to be added
			tableDiff.ColumnsAdded = append(tableDiff.ColumnsAdded, *desiredCol)
		}
	}

	// Find dropped columns
	for name, actualCol := range actualCols {
		if _, exists := desiredCols[name]; !exists {
			tableDiff.ColumnsDropped = append(tableDiff.ColumnsDropped, *actualCol)
		}
	}

	// Check primary key changes
	if !d.slicesEqual(desired.PrimaryKey, actual.PrimaryKey) {
		tableDiff.PrimaryKeyChanged = true
		tableDiff.OldPrimaryKey = actual.PrimaryKey
		tableDiff.NewPrimaryKey = desired.PrimaryKey
	}

	return tableDiff, nil
}

// diffColumns compares two columns and returns differences
func (d *DefaultSchemaDiffer) diffColumns(desired *Column, actual *Column) *ColumnDiff {
	changes := make([]string, 0)

	colDiff := &ColumnDiff{
		ColumnName: desired.Name,
		Changes:    changes,
	}

	hasChanges := false

	// Compare type
	if !d.typesEqual(desired.Type, actual.Type) {
		colDiff.OldType = actual.Type
		colDiff.NewType = desired.Type
		colDiff.Changes = append(colDiff.Changes, fmt.Sprintf("type changed from %s to %s", actual.Type, desired.Type))
		hasChanges = true
	}

	// Compare nullable
	if desired.Nullable != actual.Nullable {
		colDiff.OldNullable = actual.Nullable
		colDiff.NewNullable = desired.Nullable
		if desired.Nullable {
			colDiff.Changes = append(colDiff.Changes, "changed to nullable")
		} else {
			colDiff.Changes = append(colDiff.Changes, "changed to NOT NULL")
		}
		hasChanges = true
	}

	// Compare default
	if !d.defaultsEqual(desired.Default, actual.Default) {
		colDiff.OldDefault = actual.Default
		colDiff.NewDefault = desired.Default
		colDiff.Changes = append(colDiff.Changes, fmt.Sprintf("default changed from %v to %v", actual.Default, desired.Default))
		hasChanges = true
	}

	// Compare length
	if desired.Length != actual.Length && desired.Length > 0 {
		colDiff.Changes = append(colDiff.Changes, fmt.Sprintf("length changed from %d to %d", actual.Length, desired.Length))
		hasChanges = true
	}

	// Compare precision/scale for numeric types
	if desired.Precision != actual.Precision && desired.Precision > 0 {
		colDiff.Changes = append(colDiff.Changes, fmt.Sprintf("precision changed from %d to %d", actual.Precision, desired.Precision))
		hasChanges = true
	}

	if desired.Scale != actual.Scale && desired.Scale > 0 {
		colDiff.Changes = append(colDiff.Changes, fmt.Sprintf("scale changed from %d to %d", actual.Scale, desired.Scale))
		hasChanges = true
	}

	if !hasChanges {
		return nil
	}

	return colDiff
}

// diffIndexes compares indexes
func (d *DefaultSchemaDiffer) diffIndexes(desired []Index, actual []Index, diff *SchemaDiff) {
	desiredMap := make(map[string]*Index)
	for i := range desired {
		key := d.indexKey(&desired[i])
		desiredMap[key] = &desired[i]
	}

	actualMap := make(map[string]*Index)
	for i := range actual {
		key := d.indexKey(&actual[i])
		actualMap[key] = &actual[i]
	}

	// Find added indexes
	for key, idx := range desiredMap {
		if _, exists := actualMap[key]; !exists {
			diff.IndexesAdded = append(diff.IndexesAdded, *idx)
			diff.HasChanges = true
		}
	}

	// Find dropped indexes
	for key, idx := range actualMap {
		if _, exists := desiredMap[key]; !exists {
			diff.IndexesDropped = append(diff.IndexesDropped, *idx)
			diff.HasChanges = true
		}
	}
}

// diffConstraints compares constraints
func (d *DefaultSchemaDiffer) diffConstraints(desired []Constraint, actual []Constraint, diff *SchemaDiff) {
	desiredMap := make(map[string]*Constraint)
	for i := range desired {
		key := d.constraintKey(&desired[i])
		desiredMap[key] = &desired[i]
	}

	actualMap := make(map[string]*Constraint)
	for i := range actual {
		key := d.constraintKey(&actual[i])
		actualMap[key] = &actual[i]
	}

	// Find added constraints
	for key, con := range desiredMap {
		if _, exists := actualMap[key]; !exists {
			diff.ConstraintsAdded = append(diff.ConstraintsAdded, *con)
			diff.HasChanges = true
		}
	}

	// Find dropped constraints
	for key, con := range actualMap {
		if _, exists := desiredMap[key]; !exists {
			diff.ConstraintsDropped = append(diff.ConstraintsDropped, *con)
			diff.HasChanges = true
		}
	}
}

// diffTriggers compares triggers
func (d *DefaultSchemaDiffer) diffTriggers(desired []Trigger, actual []Trigger, diff *SchemaDiff) {
	desiredMap := make(map[string]*Trigger)
	for i := range desired {
		key := fmt.Sprintf("%s.%s", desired[i].Table, desired[i].Name)
		desiredMap[key] = &desired[i]
	}

	actualMap := make(map[string]*Trigger)
	for i := range actual {
		key := fmt.Sprintf("%s.%s", actual[i].Table, actual[i].Name)
		actualMap[key] = &actual[i]
	}

	// Find added triggers
	for key, trg := range desiredMap {
		if _, exists := actualMap[key]; !exists {
			diff.TriggersAdded = append(diff.TriggersAdded, *trg)
			diff.HasChanges = true
		}
	}

	// Find dropped triggers
	for key, trg := range actualMap {
		if _, exists := desiredMap[key]; !exists {
			diff.TriggersDropped = append(diff.TriggersDropped, *trg)
			diff.HasChanges = true
		}
	}
}

// diffViews compares views
func (d *DefaultSchemaDiffer) diffViews(desired []View, actual []View, diff *SchemaDiff) {
	desiredMap := make(map[string]*View)
	for i := range desired {
		desiredMap[desired[i].Name] = &desired[i]
	}

	actualMap := make(map[string]*View)
	for i := range actual {
		actualMap[actual[i].Name] = &actual[i]
	}

	// Find added views
	for name, view := range desiredMap {
		if _, exists := actualMap[name]; !exists {
			diff.ViewsAdded = append(diff.ViewsAdded, *view)
			diff.HasChanges = true
		}
	}

	// Find dropped views
	for name, view := range actualMap {
		if _, exists := desiredMap[name]; !exists {
			diff.ViewsDropped = append(diff.ViewsDropped, *view)
			diff.HasChanges = true
		}
	}
}

// CalculateSafety determines if changes are safe to auto-apply
func (d *DefaultSchemaDiffer) CalculateSafety(ctx context.Context, diff *SchemaDiff) bool {
	if !d.config.AllowDestructive {
		// If destructive changes not allowed, check for any
		if diff.Summary.DestructiveChanges > 0 {
			return false
		}
	}

	// Safe if no dropped tables or columns
	if len(diff.TablesDropped) > 0 {
		return false
	}

	for _, tableDiff := range diff.TablesModified {
		if len(tableDiff.ColumnsDropped) > 0 {
			return false
		}

		// Not safe if making columns NOT NULL without default
		for _, colDiff := range tableDiff.ColumnsModified {
			if colDiff.NewNullable == false && colDiff.OldNullable == true {
				return false
			}
		}
	}

	return true
}

// GetDestructiveChanges returns list of potentially destructive changes
func (d *DefaultSchemaDiffer) GetDestructiveChanges(ctx context.Context, diff *SchemaDiff) []string {
	destructive := make([]string, 0)

	// Dropped tables
	for _, table := range diff.TablesDropped {
		destructive = append(destructive, fmt.Sprintf("DROP TABLE %s (potential data loss)", table.Name))
	}

	// Dropped columns
	for _, tableDiff := range diff.TablesModified {
		for _, col := range tableDiff.ColumnsDropped {
			destructive = append(destructive, fmt.Sprintf("DROP COLUMN %s.%s (potential data loss)", tableDiff.TableName, col.Name))
		}
	}

	// Type changes that may cause data loss
	for _, tableDiff := range diff.TablesModified {
		for _, colDiff := range tableDiff.ColumnsModified {
			if d.isDestructiveTypeChange(colDiff.OldType, colDiff.NewType) {
				destructive = append(destructive, fmt.Sprintf("ALTER COLUMN %s.%s type from %s to %s (potential data truncation)",
					tableDiff.TableName, colDiff.ColumnName, colDiff.OldType, colDiff.NewType))
			}
		}
	}

	return destructive
}

// Helper methods

func (d *DefaultSchemaDiffer) hasTableChanges(tableDiff *TableDiff) bool {
	return len(tableDiff.ColumnsAdded) > 0 ||
		len(tableDiff.ColumnsDropped) > 0 ||
		len(tableDiff.ColumnsModified) > 0 ||
		tableDiff.PrimaryKeyChanged
}

func (d *DefaultSchemaDiffer) calculateSummary(diff *SchemaDiff) DiffSummary {
	summary := DiffSummary{
		TablesAdded:        len(diff.TablesAdded),
		TablesDropped:      len(diff.TablesDropped),
		TablesModified:     len(diff.TablesModified),
		IndexesAdded:       len(diff.IndexesAdded),
		IndexesDropped:     len(diff.IndexesDropped),
		ConstraintsAdded:   len(diff.ConstraintsAdded),
		ConstraintsDropped: len(diff.ConstraintsDropped),
	}

	// Count column changes
	for _, tableDiff := range diff.TablesModified {
		summary.ColumnsAdded += len(tableDiff.ColumnsAdded)
		summary.ColumnsDropped += len(tableDiff.ColumnsDropped)
		summary.ColumnsModified += len(tableDiff.ColumnsModified)
	}

	// Calculate total changes
	summary.TotalChanges = summary.TablesAdded + summary.TablesDropped + summary.TablesModified +
		summary.ColumnsAdded + summary.ColumnsDropped + summary.ColumnsModified +
		summary.IndexesAdded + summary.IndexesDropped +
		summary.ConstraintsAdded + summary.ConstraintsDropped

	// Calculate destructive changes
	summary.DestructiveChanges = summary.TablesDropped + summary.ColumnsDropped

	return summary
}

func (d *DefaultSchemaDiffer) slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	// Sort for comparison
	aCopy := make([]string, len(a))
	bCopy := make([]string, len(b))
	copy(aCopy, a)
	copy(bCopy, b)
	sort.Strings(aCopy)
	sort.Strings(bCopy)

	for i := range aCopy {
		if aCopy[i] != bCopy[i] {
			return false
		}
	}

	return true
}

func (d *DefaultSchemaDiffer) typesEqual(a, b string) bool {
	// Normalize types for comparison
	return d.normalizeType(a) == d.normalizeType(b)
}

func (d *DefaultSchemaDiffer) normalizeType(t string) string {
	// Convert to lowercase and remove spaces
	t = strings.ToLower(strings.TrimSpace(t))

	// Handle common type aliases
	switch t {
	case "int", "integer", "int4":
		return "integer"
	case "bigint", "int8":
		return "bigint"
	case "smallint", "int2":
		return "smallint"
	case "varchar", "character varying":
		return "varchar"
	case "char", "character":
		return "char"
	case "bool", "boolean":
		return "boolean"
	case "real", "float4":
		return "real"
	case "double precision", "float8":
		return "double precision"
	default:
		return t
	}
}

func (d *DefaultSchemaDiffer) defaultsEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func (d *DefaultSchemaDiffer) indexKey(idx *Index) string {
	cols := strings.Join(idx.Columns, ",")
	return fmt.Sprintf("%s.%s.%s", idx.Table, idx.Name, cols)
}

func (d *DefaultSchemaDiffer) constraintKey(con *Constraint) string {
	cols := strings.Join(con.Columns, ",")
	return fmt.Sprintf("%s.%s.%s.%s", con.Table, con.Name, con.Type, cols)
}

func (d *DefaultSchemaDiffer) isDestructiveTypeChange(oldType, newType string) bool {
	oldNorm := d.normalizeType(oldType)
	newNorm := d.normalizeType(newType)

	// Same type is not destructive
	if oldNorm == newNorm {
		return false
	}

	// Widening conversions are safe
	safeConversions := map[string][]string{
		"smallint":        {"integer", "bigint"},
		"integer":         {"bigint"},
		"real":            {"double precision"},
		"varchar":         {"text"},
	}

	if allowed, exists := safeConversions[oldNorm]; exists {
		for _, safe := range allowed {
			if newNorm == safe {
				return false
			}
		}
	}

	// All other conversions are potentially destructive
	return true
}
