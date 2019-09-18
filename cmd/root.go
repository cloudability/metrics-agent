package cmd

import (
	util "github.com/cloudability/metrics-agent/util"
	cldyVersion "github.com/cloudability/metrics-agent/version"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

//RootCmd is the cobra root command to be executed
var RootCmd = &cobra.Command{
	Use:              "metrics-agent [command] [flags]",
	Short:            "Starts the Cloudability Metrics Agent",
	Long:             `Starts the Cloudability Metrics Agent for the configured metrics collectors and polling interval.`,
	Args:             cobra.MinimumNArgs(1),
	TraverseChildren: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return util.SetupLogger()
	},
	Run: func(cmd *cobra.Command, args []string) {
	},
}

func init() {

	RootCmd.PersistentFlags().String(
		"log_level",
		"INFO",
		"Log level to run the agent at (INFO,WARN,DEBUG)",
	)

	RootCmd.PersistentFlags().String(
		"log_format",
		"PLAIN",
		"Format for log output (JSON,TXT)",
	)

	// set version flag
	RootCmd.Version = cldyVersion.VERSION

	viper.BindPFlag("log_level", RootCmd.PersistentFlags().Lookup("log_level"))
	viper.BindPFlag("log_format", RootCmd.PersistentFlags().Lookup("log_format"))

}

// Execute metrics-agent with arguments
func Execute() {
	err := RootCmd.Execute()
	if err != nil {
		log.Fatalln("Unable to execute :", err)
	}
}
