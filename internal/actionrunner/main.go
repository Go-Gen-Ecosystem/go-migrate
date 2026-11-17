// SPDX-License-Identifier: Apache-2.0
// @author Admilson B. F. Cossa

// The composite action runs this file with the Go toolchain it uses to install
// gomigrate. Keep it stand-alone and free of module dependencies.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	os.Exit(run(os.Getenv("INPUT_COMMAND"), os.Stdout, os.Stderr))
}

func run(command string, stdout, stderr io.Writer) int {
	args, err := splitCommand(command)
	if err == nil {
		// NUL records preserve empty arguments, whitespace, and newlines when
		// the action reads them into a Bash array.
		for _, arg := range args {
			if _, err = io.WriteString(stdout, arg+"\x00"); err != nil {
				break
			}
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "gomigrate action: command input: %v\n", err)
		return 1
	}
	return 0
}

// splitCommand applies shell-style quoting and escaping without expanding or
// interpreting shell syntax. It separates arguments only at unquoted whitespace.
func splitCommand(command string) ([]string, error) {
	var args []string
	var word strings.Builder
	var quote byte
	started := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote == 0 && (c == '\'' || c == '"'):
			quote = c
			started = true
		case c == '\\' && quote != '\'':
			if i+1 == len(command) {
				return nil, errors.New("trailing backslash")
			}
			next := command[i+1]
			if next == '\n' {
				i++
				continue
			}
			if quote == 0 || strings.ContainsRune("\"\\$`", rune(next)) {
				c = next
				i++
			}
			word.WriteByte(c)
			started = true
		case quote == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			if started {
				args = append(args, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if started {
		args = append(args, word.String())
	}
	return args, nil
}
