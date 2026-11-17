// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package config

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	db2 "github.com/go-db2/go-db2"
)

func TestDB2DefaultsToVerifiedTLS(t *testing.T) {
	for _, input := range []Database{
		{DSN: "db2://test:p%40ss%2Fword@localhost/GTMTEST"},
		{Driver: "db2luw", Name: "GTMTEST", User: "test", Password: "p@ss/word"},
		{DSN: "db2s://test:p%40ss%2Fword@localhost/GTMTEST"},
	} {
		name, dsn, err := Resolve(input)
		if err != nil || name != "db2" {
			t.Fatalf("dialect = %q, %v", name, err)
		}
		driverConfig, err := db2.ParseDSN(dsn)
		if err != nil || !driverConfig.UseSSL || driverConfig.Port != 50001 || driverConfig.AllowPlaintextAuth || driverConfig.Password != "p@ss/word" {
			t.Fatal("Db2 TLS settings or credentials did not resolve as intended")
		}
	}
	for _, raw := range []string{
		"db2s://test:secret%40word@localhost/GTMTEST?tls=false",
		"db2://test:secret%40word@localhost/GTMTEST?tls=false",
		"db2://test:secret%40word@localhost/GTMTEST?tls=true&tls=false",
	} {
		_, _, err := Open(t.Context(), Database{DSN: raw})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("Db2 invalid TLS configuration did not return a credential-safe error")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	endpoint := &url.URL{Scheme: "db2", Host: "localhost", Path: "/GTMTEST", User: url.UserPassword("test", "unused")}
	if _, _, err := Open(ctx, Database{DSN: endpoint.String()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Db2 connection = %v", err)
	}
}
