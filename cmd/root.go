package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// RootCmd is the cobra root command to be executed
var RootCmd = &cobra.Command{
	Use:   "metrics-agent [command] [flags]",
	Short: "Starts the Cloudability Metrics Agent",
	Long:  `Starts the Cloudability Metrics Agent for the configured metrics collectors and polling interval.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("")
	},
}

// Execute metrics-agent with arguments
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		log.Fatalln("Unable to execute :", err)
	}
}
