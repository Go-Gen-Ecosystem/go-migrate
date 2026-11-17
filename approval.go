// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

const approvalVersion = "v1"

// approvalDigest frames variable-length fields independently, including any
// delimiter characters used in a quoted SQL identifier or path.
func approvalDigest(fields []string) string {
	h := sha256.New()
	var length [binary.MaxVarintLen64]byte
	for _, field := range fields {
		n := binary.PutUvarint(length[:], uint64(len(field)))
		_, _ = h.Write(length[:n])
		_, _ = io.WriteString(h, field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (m *Migrator) approvalTarget(ctx context.Context, conn *sql.Conn, table dialect.Table) (string, error) {
	fields, err := m.dialect.ApprovalTarget(ctx, conn, table)
	if err != nil {
		return "", err
	}
	return approvalDigest(append(fields, m.opts.approvalEndpoint)), nil
}

func approvedID(target string, c *catalog, dir Direction, units []*unit, s *snapshot, baseline string) string {
	fields := []string{target, c.planID(dir, units), baseline}
	if dir == Down {
		for _, u := range units {
			// MARK_RAN is reverted by forgetting the row, not running SQL.
			fields = append(fields, string(s.byKey[u.key].Kind))
		}
	}
	return approvalVersion + ":" + target + ":" + approvalDigest(fields)
}

// checkApprovalTarget runs both immediately before lock acquisition (before
// SQLite sets up its lease) and under lock on the very same SQL connection.
func (m *Migrator) checkApprovalTarget(ctx context.Context, conn *sql.Conn, table dialect.Table, id string) (string, error) {
	rest, found := strings.CutPrefix(id, approvalVersion+":")
	approved, plan, split := strings.Cut(rest, ":")
	if !found || !split || len(approved) != sha256.Size*2 || len(plan) != sha256.Size*2 {
		return "", ErrPlanChanged
	}
	target, err := m.approvalTarget(ctx, conn, table)
	if err != nil {
		if errors.Is(err, dialect.ErrApprovalTarget) {
			return "", ErrPlanChanged
		}
		return "", err
	}
	if target != approved {
		return "", ErrPlanChanged
	}
	return target, nil
}
