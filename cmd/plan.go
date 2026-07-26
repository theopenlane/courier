package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/theopenlane/courier/pkg/engine"
)

// changesExitCode is the exit code returned by plan --detailed-exitcode when changes exist
const changesExitCode = 2

var (
	// flagPlanJSON renders the plan as JSON
	flagPlanJSON bool
	// flagDetailedExit exits with changesExitCode when the plan contains changes
	flagDetailedExit bool
)

// planCmd shows the changes apply would make to Openlane
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "show the changes apply would make to Openlane",
	RunE: func(cmd *cobra.Command, _ []string) error {
		_, plan, err := computePlan(cmd.Context())
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

		if flagDetailedExit && plan.HasChanges() {
			os.Exit(changesExitCode)
		}

		return nil
	},
}

// init registers the plan command
func init() {
	planCmd.Flags().BoolVar(&flagPlanJSON, "json", false, "output the plan as JSON")
	planCmd.Flags().BoolVar(&flagDetailedExit, "detailed-exitcode", false, "exit with code 2 when the plan contains changes")
	rootCmd.AddCommand(planCmd)
}

// computePlan loads the workspace, fetches remote state, and diffs them
func computePlan(ctx context.Context) (*engine.Client, *engine.Plan, error) {
	client, settings, err := newClient()
	if err != nil {
		return nil, nil, err
	}

	ws, err := engine.LoadWorkspace(settings.Dir)
	if err != nil {
		return nil, nil, err
	}

	state, err := client.FetchState(ctx)
	if err != nil {
		return nil, nil, err
	}

	plan, err := engine.ComputePlan(ws, state)
	if err != nil {
		return nil, nil, err
	}

	return client, plan, nil
}

// renderPlan prints a human readable plan
func renderPlan(plan *engine.Plan) {
	if !plan.HasChanges() {
		fmt.Println("no changes, Openlane matches the workspace")
	}

	for _, create := range plan.CreateControls {
		fmt.Printf("+ create control %s\n", create.Doc.RefCode)
	}

	for _, update := range plan.UpdateControls {
		fmt.Printf("~ update control %s\n", update.Doc.RefCode)
		renderDiffs(update.Diffs)
	}

	for _, add := range plan.MappingAdds {
		fmt.Printf("+ map control %s -> %s\n", add.RefCode, strings.Join(add.Targets, ", "))
	}

	for _, create := range plan.CreatePolicies {
		fmt.Printf("+ create policy %s\n", create.Policy.Name)
	}

	for _, update := range plan.UpdatePolicies {
		fmt.Printf("~ update policy %s\n", update.Policy.Name)
		renderDiffs(update.Diffs)

		if update.BodyChanged {
			fmt.Println("    body: markdown differs from policy details")
		}

		if len(update.AddControls) > 0 {
			fmt.Printf("    link controls: %s\n", strings.Join(update.AddControls, ", "))
		}
	}

	renderDrift(plan)
}

// renderDrift prints records present in Openlane but not in the workspace
func renderDrift(plan *engine.Plan) {
	total := len(plan.DriftControls) + len(plan.DriftMappings) + len(plan.DriftPolicies) + len(plan.DriftPolicyControls)
	if total == 0 {
		return
	}

	fmt.Println("drift, present in Openlane but not in the workspace, apply never deletes:")

	for _, rc := range plan.DriftControls {
		fmt.Printf("  control %s (%s)\n", rc.RefCode, rc.ID)
	}

	for _, m := range plan.DriftMappings {
		fmt.Printf("  mapping %s -> %s\n", m.RefCode, strings.Join(m.Targets, ", "))
	}

	for _, rp := range plan.DriftPolicies {
		fmt.Printf("  policy %s (%s)\n", rp.Name, rp.ID)
	}

	for _, pd := range plan.DriftPolicyControls {
		fmt.Printf("  policy %s linked controls: %s\n", pd.Name, strings.Join(pd.Targets, ", "))
	}
}

// renderDiffs prints field-level changes indented under their parent line
func renderDiffs(diffs []engine.FieldDiff) {
	for _, diff := range diffs {
		fmt.Printf("    %s: %q -> %q\n", diff.Field, diff.Old, diff.New)
	}
}
