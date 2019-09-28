package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "lifecycle-manager",
	Short: "lifecycle-manager makes AWS scaling event on Kubernetes graceful using lifecycle hooks",
	Long: `lifecycle-manager is a service that can be deployed to a Kubernetes cluster in order to make AWS autoscaling events more graceful using draining

lifecycle-manager serve --kubectl-path /usr/local/bin/kubectl --queue-name test-queue --region us-west-2`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize()
}
