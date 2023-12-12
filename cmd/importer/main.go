package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/qiujian16/capi-importer/pkg/importers"
	"github.com/qiujian16/capi-importer/pkg/version"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
)

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	command := newImporterCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newImporterCommand() *cobra.Command {
	opts := importers.NewImporterOptions()
	cmdConfig := controllercmd.NewControllerCommandConfig("importer", version.Get(), opts.RunImporterController)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "manager"
	cmd.Short = "Start the importer"
	opts.AddFlags(cmd.Flags())
	return cmd
}
