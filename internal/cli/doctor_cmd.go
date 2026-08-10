package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"cyber-code/internal/cyberagent"
	"cyber-code/internal/doctor"
)

func newDoctorCommand(environment *commandEnvironment) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "doctor", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			report := doctor.Run(environment.ctx, doctor.Options{ConfigFile: environment.configFile, StateDir: environment.stateDir})
			report.Checks = append(report.Checks, cyberAgentDoctorCheck())
			if jsonOutput {
				encoder := json.NewEncoder(environment.stdout)
				encoder.SetEscapeHTML(false)
				if err := encoder.Encode(report); err != nil {
					return err
				}
			} else {
				for _, check := range report.Checks {
					_, _ = fmt.Fprintf(environment.stdout, "%s\t%s\t%s\n", check.Status, check.Name, check.Message)
					if check.Remediation != "" {
						_, _ = fmt.Fprintf(environment.stdout, "\tremediation: %s\n", check.Remediation)
					}
				}
			}
			if !report.Healthy {
				return exitStatus{code: 2}
			}
			return nil
		},
	}
	command.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON")
	return command
}

func cyberAgentDoctorCheck() doctor.Check {
	result, err := cyberagent.Discover(cyberagent.DiscoveryOptions{Environment: os.Environ()})
	if err != nil {
		return doctor.Check{
			Name: "cyber_agent", Status: doctor.StatusWarn, Message: "cyber-agent runtime is unavailable",
			Remediation: "Install cyber-agent on PATH or set CYBER_AGENT_PATH to its executable before selecting the Security Runtime.",
		}
	}
	return doctor.Check{Name: "cyber_agent", Status: doctor.StatusPass, Message: result.Path + " (source=" + result.Source + ")"}
}
