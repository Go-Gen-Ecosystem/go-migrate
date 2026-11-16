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

	"github.com/oracle/go-oracledb/v26/oracle"
	oracleerrors "github.com/oracle/go-oracledb/v26/oracle/errors"
)

const oracleTCPPort = 1521
const oracleTCPSPort = 2484

var oracleEndpointName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var oracleErrorCode = regexp.MustCompile(`^(?:ORA|OGD)-\d{5}$`)

func oracleDSN(db Database) (string, error) {
	u := &url.URL{Scheme: "oracle", Host: or(db.Host, defaultHost), Path: "/" + db.Name,
		User: url.UserPassword(db.User, db.Password)}
	host := u.Host
	port := db.Port
	if db.DSN != "" {
		parsed, err := url.Parse(db.DSN)
		if err != nil || !strings.EqualFold(parsed.Scheme, "oracle") || parsed.Fragment != "" {
			return "", errors.New("invalid oracle:// URL")
		}
		u = parsed
		u.Scheme = "oracle"
		host = u.Hostname()
		if u.Port() != "" {
			port, err = strconv.Atoi(u.Port())
			if err != nil {
				return "", errors.New("invalid Oracle port")
			}
		}
	} else if db.Name == "" {
		return "", ErrNoDatabase
	}
	params, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", errors.New("invalid oracle:// URL parameters")
	}
	for key, value := range db.Params {
		if !params.Has(key) {
			params.Set(key, value)
		}
	}
	for key, values := range params {
		if len(values) != 1 {
			return "", errors.New("duplicate Oracle connection parameter")
		}
		switch key {
		case "protocol", "wallet_location", "ssl_server_cert_dn":
		default:
			return "", errors.New("unsupported Oracle connection parameter")
		}
	}
	if db.SSLMode != "" {
		return "", errors.New("set Oracle transport using protocol in the oracle:// URL or database.params")
	}
	protocol := or(params.Get("protocol"), "tcps")
	defaultPort := oracleTCPSPort
	switch protocol {
	case "tcp":
		defaultPort = oracleTCPPort
	case "tcps":
	default:
		return "", errors.New("set Oracle protocol to tcp or tcps")
	}
	if net.ParseIP(host) == nil && !oracleEndpointName.MatchString(host) {
		return "", errors.New("invalid Oracle host")
	}
	if !oracleEndpointName.MatchString(strings.TrimPrefix(u.Path, "/")) || port < 0 || port > 65535 {
		return "", errors.New("invalid Oracle service name or port")
	}
	if password, _ := u.User.Password(); u.User.Username() == "" || password == "" {
		return "", errors.New("set both Oracle username and password")
	}
	u.Host = net.JoinHostPort(host, strconv.Itoa(orInt(port, defaultPort)))
	params.Set("protocol", protocol)
	u.RawQuery = params.Encode()
	return u.String(), nil
}

func openOraclePool(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	params := u.Query()
	config := oracle.NewOracleDriverConfig()
	// Use the driver's structured credentials, so password delimiters are
	// never interpreted as Easy Connect syntax or included in its DSN.
	config.Credentials.User = u.User.Username()
	config.Credentials.Password, _ = u.User.Password()
	config.ConnectionProperties.WalletLocation = params.Get("wallet_location")
	config.ConnectionProperties.SSLServerCertDN = params.Get("ssl_server_cert_dn")
	config.ConnectDescriptor = params.Get("protocol") + "://" + u.Host + u.Path +
		"?ssl_server_dn_match=true&ssl_allow_weak_dn_match=false&failover=false&retry_count=0"
	connector, err := oracle.NewOracleConnector(config)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func oracleConnectError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return fmt.Errorf("connect to Oracle: %w", cause)
	}
	var code string
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if driverError, ok := cause.(oracleerrors.SQLError); ok && oracleErrorCode.MatchString(driverError.ErrorCode()) {
			code = driverError.ErrorCode()
		}
	}
	if code != "" {
		return fmt.Errorf("connect to Oracle: %s", code)
	}
	return errors.New("connect to Oracle: check server address, service, credentials, and TLS settings")
}
