// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"

	db2 "github.com/go-db2/go-db2"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/microsoft/go-mssqldb/msdsn"

	"github.com/go-gen-ecosystem/go-migrate/internal/dialect"
)

// approvalEndpoint supplements server metadata with the configured route. Only
// take transport fields from driver configuration, never passwords, usernames,
// connection strings or arbitrary connection parameters.
func approvalEndpoint(name, dsn string) (string, error) {
	fields, err := routeFields(name, dsn)
	if err != nil {
		return "", errors.New("cannot identify the configured database endpoint")
	}
	return encodeEndpoint(fields), nil
}

func encodeEndpoint(fields []string) string {
	data, _ := json.Marshal(fields) // []string cannot fail to marshal
	return string(data)
}

func routeFields(name, dsn string) ([]string, error) {
	switch name {
	case dialect.MySQL:
		m, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return nil, err
		}
		return []string{m.Net, m.Addr}, nil
	case dialect.SQLServer:
		s, err := msdsn.Parse(dsn)
		if err != nil {
			return nil, err
		}
		return []string{s.Host, strconv.FormatUint(s.Port, 10), s.Instance,
			s.FailOverPartner, strconv.FormatUint(s.FailOverPort, 10)}, nil
	case dialect.Oracle:
		u, err := url.Parse(dsn)
		if err != nil {
			return nil, err
		}
		return []string{u.Host, u.Query().Get("protocol")}, nil
	case dialect.DB2:
		d, err := db2.ParseDSN(dsn)
		if err != nil {
			return nil, err
		}
		return []string{d.Host, strconv.Itoa(d.Port)}, nil
	default:
		// SQLite identity is the files SQLite reports, not its URI.
		return nil, nil
	}
}
