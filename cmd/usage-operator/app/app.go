package app

import (
	"context"
	"fmt"
	"os"

	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/spf13/cobra"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

func NewUsageOperatorCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "usage-operator",
		Short: "Commands for interacting with the usage-operator",
	}
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	so := &SharedOptions{
		RawSharedOptions: &RawSharedOptions{},
	}

	so.AddPersistentFlags(cmd)
	cmd.AddCommand(NewInitCommand(so))
	cmd.AddCommand(NewRunCommand(so))
	cmd.AddCommand(NewUninstallCommand(so))

	return cmd
}

type RawSharedOptions struct {
	DryRun bool `json:"dry-run"`
}

type SharedOptions struct {
	*RawSharedOptions

	// fields filled in Complete()
	Log logging.Logger
}

func (o *SharedOptions) AddPersistentFlags(cmd *cobra.Command) {
	// logging
	logging.InitFlags(cmd.PersistentFlags())
	// misc
	cmd.PersistentFlags().BoolVar(&o.DryRun, "dry-run", false, "If set, the command aborts after evaluation of the given flags.")
}

func (o *SharedOptions) Complete() error {
	// build logger
	log, err := logging.GetLogger()
	if err != nil {
		return err
	}
	o.Log = log
	ctrl.SetLogger(o.Log.Logr())

	return nil
}

func (o *SharedOptions) PrintRaw(cmd *cobra.Command) {
	data, err := yaml.Marshal(o.RawSharedOptions)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling raw shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}

func (o *SharedOptions) PrintCompleted(cmd *cobra.Command) {
	raw := map[string]any{}
	data, err := yaml.Marshal(raw)
	if err != nil {
		cmd.Println(fmt.Errorf("error marshalling completed shared options: %w", err).Error())
		return
	}
	cmd.Print(string(data))
}
