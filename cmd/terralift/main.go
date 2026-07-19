// Command terralift brings existing cloud infrastructure under Terraform:
// enumerate -> born-correct export -> reconcile into a plan-clean module repo ->
// package. Brownfield to greenfield. This file wires the root CLI (cobra); the run
// commands live in run.go and the phase pipeline in the internal/ packages.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	// Blank imports register each cloud provider via its init().
	_ "github.com/cyberproaustin/terralift/internal/providers/aws"
	_ "github.com/cyberproaustin/terralift/internal/providers/azure"
	_ "github.com/cyberproaustin/terralift/internal/providers/gcp"
)

// version is stamped at build time: -ldflags "-X main.version=v1.0.0".
var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "terralift",
		Short: "Bring existing cloud infrastructure under Terraform — brownfield to greenfield.",
		Long: bannerText(false) + `
TerraLift onboards live AWS, GCP, and Azure infrastructure into a plan-clean
Terraform repo: it enumerates what's running, authors born-correct import blocks
and HCL, reconciles it to a module layout, and verifies the round-trip.

  terralift onboard --cloud gcp --scope my-project-id
  terralift onboard --cloud aws --scope 123456789012
  terralift clone   --cloud azure --scope <sub-id> --resource-groups rg1,rg2`,
		// Print the error (once) but not a full usage dump on every failure; main()
		// just sets the exit code.
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version, // enables --version
	}
	root.SetVersionTemplate("terralift {{.Version}}\n")
	root.AddCommand(onboardCmd(), cloneCmd(), versionCmd(), bannerCmd())
	return root
}

func bannerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "banner",
		Short: "Print the TerraLift banner.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			// Colored on a terminal, plain when piped/redirected.
			fmt.Fprint(cmd.OutOrStdout(), bannerText(isTTY(os.Stdout)))
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the TerraLift version.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Printf("terralift %s\n", version)
		},
	}
}
