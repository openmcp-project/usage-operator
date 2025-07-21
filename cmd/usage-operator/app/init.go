package app

import (
	"context"
	"fmt"

	crdutil "github.com/openmcp-project/controller-utils/pkg/crds"
	apiconst "github.com/openmcp-project/openmcp-operator/api/constants"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openmcp-project/usage-operator/api/crds"
	"github.com/openmcp-project/usage-operator/internal/helper"
)

func NewInitCommand(so *SharedOptions) *cobra.Command {
	opts := &InitOptions{
		SharedOptions: so,
	}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the usage operator",
		Run: func(cmd *cobra.Command, args []string) {
			if err := opts.Complete(cmd.Context()); err != nil {
				panic(fmt.Errorf("error completing options: %w", err))
			}
			opts.PrintCompletedOptions(cmd)
			if opts.DryRun {
				cmd.Println("=== END OF DRY RUN ===")
				return
			}
			if err := opts.Run(cmd.Context()); err != nil {
				panic(err)
			}
		},
	}
	opts.AddFlags(cmd)

	return cmd
}

type InitOptions struct {
	*SharedOptions
}

func (o *InitOptions) AddFlags(cmd *cobra.Command) {
}

func (o *InitOptions) Complete(ctx context.Context) error {
	if err := o.SharedOptions.Complete(); err != nil {
		return err
	}
	return nil
}

func (o *InitOptions) Run(ctx context.Context) error {
	log := o.Log.WithName("main")
	log.Info("Environment", "value", o.Environment)

	// apply CRDs
	crdManager := crdutil.NewCRDManager(apiconst.ClusterLabel, crds.CRDs)

	cluster, err := helper.GetOnboardingCluster(ctx, log, o.PlatformCluster.Client())
	if err != nil {
		return fmt.Errorf("error when getting onboarding cluster: %w", err)
	}

	if err := cluster.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	crdManager.AddCRDLabelToClusterMapping("onboarding", cluster)

	if err := crdManager.CreateOrUpdateCRDs(ctx, &log); err != nil {
		return fmt.Errorf("error creating/updating CRDs: %w", err)
	}

	log.Info("Finished init command")
	return nil
}

func (o *InitOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *InitOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
