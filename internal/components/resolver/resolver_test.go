/**
 * @file resolver_test.go
 * @description Behavioral tests for the dependency resolver: topological ordering,
 * missing-dependency detection, cycle detection, and precondition parsing.
 * @author Admilson B. F. Cossa
 */

package resolver

import (
	"context"
	"testing"

	"github.com/go-gen-ecosystem/go-migrate/internal/components/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mig builds a migration with the given ID and dependencies.
func mig(id string, deps ...string) *types.Migration {
	return &types.Migration{ID: id, Dependencies: deps}
}

// indexOf returns the position of id within the ordered migration slice, or -1.
func indexOf(order []*types.Migration, id string) int {
	for i, m := range order {
		if m.ID == id {
			return i
		}
	}
	return -1
}

func TestNewDefaultDependencyResolver_Defaults(t *testing.T) {
	r := NewDefaultDependencyResolver(nil)
	require.NotNil(t, r)
	assert.False(t, r.config.AllowCircularDependencies)
	assert.Equal(t, 100, r.config.MaxDepth)
	assert.True(t, r.config.StrictMode)
}

func TestResolveDependencies_LinearChain(t *testing.T) {
	// c depends on b, b depends on a => order must be a, b, c.
	migrations := []*types.Migration{
		mig("c", "b"),
		mig("a"),
		mig("b", "a"),
	}

	r := NewDefaultDependencyResolver(nil)
	ordered, err := r.ResolveDependencies(context.Background(), migrations)
	require.NoError(t, err)
	require.Len(t, ordered, 3)

	assert.Less(t, indexOf(ordered, "a"), indexOf(ordered, "b"))
	assert.Less(t, indexOf(ordered, "b"), indexOf(ordered, "c"))
}

func TestResolveDependencies_Diamond(t *testing.T) {
	// d depends on b and c; b and c both depend on a.
	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	migrations := []*types.Migration{
		mig("d", "b", "c"),
		mig("b", "a"),
		mig("c", "a"),
		mig("a"),
	}

	r := NewDefaultDependencyResolver(nil)
	ordered, err := r.ResolveDependencies(context.Background(), migrations)
	require.NoError(t, err)
	require.Len(t, ordered, 4)

	ai := indexOf(ordered, "a")
	bi := indexOf(ordered, "b")
	ci := indexOf(ordered, "c")
	di := indexOf(ordered, "d")

	assert.Less(t, ai, bi)
	assert.Less(t, ai, ci)
	assert.Less(t, bi, di)
	assert.Less(t, ci, di)
}

func TestGetExecutionOrder_Deterministic(t *testing.T) {
	// Independent nodes must be emitted in sorted (deterministic) order.
	migrations := []*types.Migration{mig("z"), mig("a"), mig("m")}

	r := NewDefaultDependencyResolver(nil)
	o1, err := r.GetExecutionOrder(context.Background(), migrations)
	require.NoError(t, err)
	o2, err := r.GetExecutionOrder(context.Background(), migrations)
	require.NoError(t, err)

	ids1 := []string{o1[0].ID, o1[1].ID, o1[2].ID}
	ids2 := []string{o2[0].ID, o2[1].ID, o2[2].ID}
	assert.Equal(t, []string{"a", "m", "z"}, ids1)
	assert.Equal(t, ids1, ids2, "ordering must be deterministic across runs")
}

func TestValidateDependencies_MissingDependency(t *testing.T) {
	migrations := []*types.Migration{mig("b", "ghost")}

	r := NewDefaultDependencyResolver(nil)
	err := r.ValidateDependencies(context.Background(), migrations)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestValidateDependencies_AllPresent(t *testing.T) {
	migrations := []*types.Migration{mig("a"), mig("b", "a")}

	r := NewDefaultDependencyResolver(nil)
	assert.NoError(t, r.ValidateDependencies(context.Background(), migrations))
}

func TestDetectCycles_DirectCycle(t *testing.T) {
	// a -> b -> a
	migrations := []*types.Migration{mig("a", "b"), mig("b", "a")}

	r := NewDefaultDependencyResolver(nil)
	cycles, err := r.DetectCycles(context.Background(), migrations)
	require.NoError(t, err)
	assert.NotEmpty(t, cycles, "a<->b cycle must be detected")
}

func TestDetectCycles_SelfLoop(t *testing.T) {
	migrations := []*types.Migration{mig("a", "a")}

	r := NewDefaultDependencyResolver(nil)
	cycles, err := r.DetectCycles(context.Background(), migrations)
	require.NoError(t, err)
	assert.NotEmpty(t, cycles, "self-dependency is a cycle")
}

func TestDetectCycles_NoCycle(t *testing.T) {
	migrations := []*types.Migration{mig("a"), mig("b", "a"), mig("c", "b")}

	r := NewDefaultDependencyResolver(nil)
	cycles, err := r.DetectCycles(context.Background(), migrations)
	require.NoError(t, err)
	assert.Empty(t, cycles)
}

func TestResolveDependencies_CycleRejected(t *testing.T) {
	migrations := []*types.Migration{mig("a", "b"), mig("b", "a")}

	r := NewDefaultDependencyResolver(nil)
	_, err := r.ResolveDependencies(context.Background(), migrations)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestResolveDependencies_CycleAllowedByConfig(t *testing.T) {
	// When circular dependencies are explicitly allowed, validation passes,
	// but the topological sort in GetExecutionOrder still cannot order a cycle.
	migrations := []*types.Migration{mig("a", "b"), mig("b", "a")}

	r := NewDefaultDependencyResolver(&ResolverConfig{AllowCircularDependencies: true})
	// Validation is permitted.
	assert.NoError(t, r.ValidateDependencies(context.Background(), migrations))
	// Ordering still fails because a true cycle cannot be topologically sorted.
	_, err := r.GetExecutionOrder(context.Background(), migrations)
	assert.Error(t, err)
}

func TestGetExecutionOrder_Empty(t *testing.T) {
	r := NewDefaultDependencyResolver(nil)
	ordered, err := r.GetExecutionOrder(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, ordered)
}

// --- PreconditionResolver ---

func TestPreconditionResolver_NoPreconditions(t *testing.T) {
	p := NewPreconditionResolver(nil, nil)
	m := &types.Migration{ID: "x"}
	assert.NoError(t, p.ValidatePreconditions(context.Background(), m))
}

func TestPreconditionResolver_KnownTypesPass(t *testing.T) {
	// The stub implementations of the known precondition types all return nil.
	p := NewPreconditionResolver(nil, nil)
	m := &types.Migration{
		ID: "x",
		Preconditions: []types.Precondition{
			{Type: "table_exists", SQL: "users"},
			{Type: "column_exists", SQL: "users.id"},
			{Type: "index_exists", SQL: "idx_users"},
			{Type: "data_exists", SQL: "SELECT 1"},
			{Type: "schema_version", SQL: "1.0.0"},
		},
	}
	assert.NoError(t, p.ValidatePreconditions(context.Background(), m))
}

func TestPreconditionResolver_UnknownType(t *testing.T) {
	p := NewPreconditionResolver(nil, nil)
	m := &types.Migration{
		ID:            "x",
		Preconditions: []types.Precondition{{Type: "bogus", SQL: "param"}},
	}
	err := p.ValidatePreconditions(context.Background(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown precondition type")
}

func TestPreconditionResolver_ColumnExistsBadFormat(t *testing.T) {
	p := NewPreconditionResolver(nil, nil)
	// column_exists expects "table.column"; a bare value with no dot fails.
	m := &types.Migration{
		ID:            "x",
		Preconditions: []types.Precondition{{Type: "column_exists", SQL: "nodotshere"}},
	}
	err := p.ValidatePreconditions(context.Background(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid table.column format")
}
