package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/openmcp-project/controller-utils/pkg/resources"
	"github.com/openmcp-project/openmcp-operator/api/install"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openmcp-project/usage-operator/api/crds"
	"github.com/openmcp-project/usage-operator/internal/helper"
)

func NewUninstallCommand(so *SharedOptions) *cobra.Command {
	opts := &UninstallOptions{
		SharedOptions: so,
	}
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstalls the usage-operators crds",
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

type UninstallOptions struct {
	*SharedOptions
}

func (o *UninstallOptions) AddFlags(cmd *cobra.Command) {
}

func (o *UninstallOptions) Complete(ctx context.Context) error {
	if err := o.SharedOptions.Complete(); err != nil {
		return err
	}
	return nil
}

func (o *UninstallOptions) Run(ctx context.Context) error {
	log := o.Log.WithName("main")

	crdlist, err := crds.CRDs()
	if err != nil {
		return fmt.Errorf("error when getting crds: %w", err)
	}

	cluster, err := helper.GetOnboardingCluster(ctx, log, o.PlatformCluster.Client())
	if err != nil {
		return fmt.Errorf("error when getting onboarding cluster: %w", err)
	}

	if err := cluster.InitializeClient(install.InstallCRDAPIs(runtime.NewScheme())); err != nil {
		return fmt.Errorf("error initializing client: %w", err)
	}

	var errs error
	for _, crd := range crdlist {
		log.Info("uninstalling CRD", "name", crd.Name)

		m := resources.NewCRDMutator(crd)
		m.MetadataMutator().WithLabels(crd.Labels).WithAnnotations(crd.Annotations)
		err = resources.DeleteResource(ctx, cluster.Client(), m)
		errs = errors.Join(errs, err)
	}

	return errs
}

func (o *UninstallOptions) PrintCompleted(cmd *cobra.Command) {}

func (o *UninstallOptions) PrintCompletedOptions(cmd *cobra.Command) {
	cmd.Println("########## COMPLETED OPTIONS START ##########")
	o.SharedOptions.PrintCompleted(cmd)
	o.PrintCompleted(cmd)
	cmd.Println("########## COMPLETED OPTIONS END ##########")
}
