package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"local/loop/internal/app"
	"local/loop/internal/control"
	"local/loop/internal/profile"
)

// Standard-library flags stop at the first positional argument. Move flags
// ahead of command words so both `--json status` and `status --json` work.
func commandFlags(flags *flag.FlagSet, args []string) []string {
	var options, words []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			words = append(words, arg)
			continue
		}
		options = append(options, arg)
		name := strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-")
		entry := flags.Lookup(name)
		if entry == nil || strings.Contains(arg, "=") {
			continue
		}
		if boolean, ok := entry.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			options = append(options, args[i])
		}
	}
	return append(options, words...)
}

func validateControl(args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "status", "connect", "disconnect", "ui":
		if len(args) == 1 {
			return nil
		}
	case "daemon":
		if len(args) == 2 && (args[1] == "start" || args[1] == "status" || args[1] == "stop") {
			return nil
		}
	case "mode":
		if len(args) == 1 || (len(args) == 2 && profile.Mode(args[1]).Valid()) {
			return nil
		}
	case "policy":
		if len(args) == 2 && args[1] == "list" {
			return nil
		}
		if len(args) == 4 && args[1] == "select" {
			return nil
		}
	}
	return errors.New("命令无效；支持 daemon start|status|stop、status、connect、disconnect、mode [direct|rule|global]、policy list、policy select <组> <节点键>、ui")
}

func startsDaemon(args []string) bool {
	return len(args) > 0 && (args[0] == "connect" || args[0] == "ui" || (len(args) > 1 && args[0] == "daemon" && args[1] == "start"))
}

func runControl(args []string, options app.Options, output io.Writer, jsonOutput, noTray, explicitProfile bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var client *control.Client
	var err error
	if startsDaemon(args) {
		client, err = control.Ensure(ctx, options, noTray)
	} else {
		client, err = control.NewClient(options.StateDir)
	}
	if err != nil {
		return writeControl(output, jsonOutput, control.Envelope{}, err)
	}
	defer client.Close()
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	state, err := client.Status(probe)
	cancelProbe()
	if err != nil {
		return writeControl(output, jsonOutput, control.Envelope{}, err)
	}
	if explicitProfile {
		if err := control.Compatible(state, options); err != nil {
			return writeControl(output, jsonOutput, control.Envelope{Status: state}, err)
		}
	}
	var operation app.Operation
	switch args[0] {
	case "status":
	case "daemon":
		if args[1] == "stop" {
			operation.Action = "shutdown"
		}
	case "connect", "disconnect":
		enabled := args[0] == "connect"
		operation = app.Operation{Action: "connect", Enabled: &enabled}
	case "mode":
		if len(args) == 2 {
			operation = app.Operation{Action: "set_mode", Mode: profile.Mode(args[1])}
		}
	case "policy":
		if args[1] == "select" {
			operation = app.Operation{Action: "select_policy", Group: args[2], Key: args[3]}
		}
	case "ui":
		operation.Action = "show_ui"
	}
	value := control.Envelope{OK: true, Status: state}
	if operation.Action != "" {
		value, err = client.Action(ctx, operation)
	}
	return writeControlResult(output, jsonOutput, value, err, args[0] == "policy")
}

func writeControl(output io.Writer, jsonOutput bool, value control.Envelope, err error) error {
	return writeControlResult(output, jsonOutput, value, err, false)
}

func writeControlResult(output io.Writer, jsonOutput bool, value control.Envelope, err error, listPolicies bool) error {
	if err != nil {
		value.OK = false
		value.Error = err.Error()
	}
	if jsonOutput {
		if writeErr := json.NewEncoder(output).Encode(value); writeErr != nil {
			return errors.Join(err, writeErr)
		}
	} else if err == nil {
		connection := "未连接"
		if value.Status.Running {
			connection = "已连接"
		}
		fmt.Fprintf(output, "后台 PID %d · %s · %s\n", value.Status.PID, connection, value.Status.Mode.Label())
		if listPolicies {
			for _, policy := range value.Status.Policies {
				fmt.Fprintf(output, "%s → %s\n", policy.Group.Name, policy.Selected)
				for _, member := range policy.Members {
					fmt.Fprintf(output, "  %s\t%s\t%s\n", member.Key, member.Name, member.Detail)
				}
			}
		}
	}
	return err
}
