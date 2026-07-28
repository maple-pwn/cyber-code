package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"claude-code-go/internal/doctor"
)

func newDoctorCommand(environment *commandEnvironment) *cobra.Command {
	var jsonOutput bool
	command := &cobra.Command{
		Use: "doctor", Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			report := doctor.Run(environment.ctx, doctor.Options{ConfigFile: environment.configFile, StateDir: environment.stateDir})
			if jsonOutput {
				encoder := json.NewEncoder(environment.stdout)
				encoder.SetEscapeHTML(false)
				if err := encoder.Encode(report); err != nil {
					return err
				}
			} else {
				for _, check := range report.Checks {
					_, _ = fmt.Fprintf(environment.stdout, "%s\t%s\t%s\n", check.Status, check.Name, check.Message)
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
