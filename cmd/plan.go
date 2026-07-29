package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/controlfile"
	"github.com/theopenlane/courier/pkg/engine"
)

// findingsExitCode is the exit code returned by plan --detailed-exitcode when the preflight has findings
const findingsExitCode = 2

var (
	// flagPlanJSON renders the preflight as JSON
	flagPlanJSON bool
	// flagDetailedExit exits with findingsExitCode when the preflight has findings
	flagDetailedExit bool
)

// planCmd preflights the files against Openlane without changing anything
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "preflight the files against Openlane without changing anything",
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, settings, err := newClient()
		if err != nil {
			return err
		}

		store, err := engine.NewStore(settings.Dir)
		if err != nil {
			return err
		}

		plan, err := client.Plan(cmd.Context(), store, selectedKinds(cmd.Flags()))
		if err != nil {
			return err
		}

		if flagPlanJSON {
			out, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return err
			}

			fmt.Println(string(out))
		} else {
			renderPlan(plan)
		}

		if flagDetailedExit && plan.HasFindings() {
			os.Exit(findingsExitCode)
		}

		return nil
	},
}

// init registers the plan command
func init() {
	registerKindFlags(planCmd.Flags())
	planCmd.Flags().BoolVar(&flagPlanJSON, "json", false, "output the preflight as JSON")
	planCmd.Flags().BoolVar(&flagDetailedExit, "detailed-exitcode", false, "exit with code 2 when the preflight has findings")
	rootCmd.AddCommand(planCmd)
}

// renderPlan prints the preflight findings
func renderPlan(plan *engine.Plan) {
	if !plan.HasFindings() {
		fmt.Println("nothing to report, the files match Openlane")

		return
	}

	for _, refCode := range plan.CreateControls {
		fmt.Printf("+ control %s will be created\n", refCode)
	}

	for _, name := range plan.CreatePolicies {
		fmt.Printf("+ policy %s will be created\n", name)
	}

	for _, ref := range plan.UnresolvedRefs {
		fmt.Printf("? %s does not resolve and will be skipped\n", ref)
	}

	total := len(plan.DriftControls) + len(plan.DriftMappings) + len(plan.DriftPolicies)
	if total > 0 {
		fmt.Println("in Openlane but not in the files, apply does not remove these:")
	}

	for _, rc := range plan.DriftControls {
		fmt.Printf("  control %s (%s)\n", rc.RefCode, rc.ID)
	}

	if len(plan.DriftMappings) > 0 {
		fmt.Printf("  mapped references: %s\n", renderGrouped(plan.DriftMappings))
	}

	for _, rp := range plan.DriftPolicies {
		fmt.Printf("  policy %s (%s)\n", rp.Name, rp.ID)
	}
}

// renderGrouped renders framework-grouped control references for display
func renderGrouped(grouped controlfile.MappedControls) string {
	frameworks := lo.Keys(grouped)

	parts := lo.Map(frameworks, func(framework string, _ int) string {
		return framework + ": " + strings.Join(grouped[framework], ", ")
	})

	return strings.Join(parts, "; ")
}
