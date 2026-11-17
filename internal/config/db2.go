// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	db2 "github.com/go-db2/go-db2"
)

var db2SQLState = regexp.MustCompile(`^[A-Z0-9]{5}$`)

func db2DSN(db Database) (string, error) {
	u := &url.URL{Scheme: "db2", Path: "/" + db.Name, User: url.UserPassword(db.User, db.Password)}
	host := or(db.Host, defaultHost)
	u.Host = host
	if db.Port != 0 {
		u.Host = net.JoinHostPort(host, strconv.Itoa(db.Port))
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if db.DSN != "" {
		parsed, err := url.Parse(db.DSN)
		if err != nil || (parsed.Scheme != "db2" && parsed.Scheme != "db2s") || parsed.Fragment != "" || parsed.Hostname() == "" {
			return "", errors.New("invalid db2:// URL")
		}
		u = parsed
	} else if db.Name == "" {
		return "", ErrNoDatabase
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("invalid Db2 URL parameters")
	}
	for key, value := range db.Params {
		if !params.Has(key) {
			params.Set(key, value)
		}
	}
	for key, values := range params {
		if len(values) != 1 {
			return "", errors.New("duplicate Db2 connection parameter")
		}
		switch key {
		case "tls", "ssl_ca", "timeout", "allow_plaintext_auth":
		default:
			return "", errors.New("unsupported Db2 connection parameter")
		}
	}
	if db.SSLMode != "" {
		return "", errors.New("set Db2 transport using tls in the db2:// URL or database.params")
	}
	useTLS, err := strconv.ParseBool(or(params.Get("tls"), "true"))
	if err != nil || (!useTLS && (u.Scheme == "db2s" || params.Has("ssl_ca"))) {
		return "", errors.New("invalid Db2 TLS configuration")
	}
	if password, _ := u.User.Password(); u.User.Username() == "" || password == "" {
		return "", errors.New("set both Db2 username and password")
	}
	params.Set("tls", strconv.FormatBool(useTLS))
	u.RawQuery = params.Encode()
	if _, err := db2.ParseDSN(u.String()); err != nil {
		return "", errors.New("invalid Db2 connection configuration")
	}
	return u.String(), nil
}

func openDB2Pool(dsn string) (*sql.DB, error) {
	config, err := db2.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	// The driver's dialer appends the port itself, so IPv6 hosts need brackets.
	if strings.Contains(config.Host, ":") {
		config.Host = "[" + config.Host + "]"
	}
	return sql.OpenDB(db2.NewConnector(config)), nil
}

func db2ConnectError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return fmt.Errorf("connect to Db2: %w", cause)
	}
	var driverError *db2.Error
	if errors.As(err, &driverError) && db2SQLState.MatchString(driverError.SQLState) {
		return fmt.Errorf("connect to Db2: SQLCODE %d (SQLSTATE %s)", driverError.SQLCode, driverError.SQLState)
	}
	return errors.New("connect to Db2: check server address, database, credentials, and TLS settings")
}
