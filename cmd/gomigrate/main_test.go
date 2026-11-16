// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestOracleDriverLeavesCLIFlagsToCobra(t *testing.T) {
	const child = "GOMIGRATE_TEST_ORACLE_CMD_CHILD"
	if os.Getenv(child) == "1" {
		// Undo the Go test harness's flag parse to reproduce the standalone
		// process state when the Oracle connector is first constructed.
		flag.CommandLine = flag.NewFlagSet("gomigrate", flag.ExitOnError)
		os.Args = []string{"gomigrate", "--dir", ".", "-o", "json", "status"}
		main()
		return
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestOracleDriverLeavesCLIFlagsToCobra$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), child+"=1",
		"GOMIGRATE_DRIVER=oracle",
		"GOMIGRATE_DSN=oracle://unused:unused@localhost/FREEPDB1?protocol=tcp",
		// An invalid driver setting stops before dialing, after the flag check.
		"ORACLE_GO_CONNECTIONPROPERTIES_CONNECTTIMEOUT=invalid")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), `"message": "invalid Oracle connection configuration"`) {
		t.Fatalf("CLI status = %v\n%s", err, out)
	}
}
