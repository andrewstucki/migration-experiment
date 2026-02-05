package crds

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/andrewstucki/migration-experiment/crds"
)

var (
	stableCRDs = []*apiextensionsv1.CustomResourceDefinition{
		crds.Old(),
		crds.New(),
	}
	experimentalCRDs = []*apiextensionsv1.CustomResourceDefinition{}
	schemes          = []func(s *runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		apiextensionsv1.AddToScheme,
	}
)

// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;create;update;patch

func Command() *cobra.Command {
	var experimental bool
	cmd := &cobra.Command{
		Use:   "crds",
		Short: "Install CRDs into the cluster",
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

			run(
				ctx,
				experimental,
			)
		},
	}

	cmd.Flags().BoolVar(&experimental, "experimental", false, "Install experimental CRDs")

	return cmd
}

func run(
	ctx context.Context,
	experimental bool,
) {
	crdType := "stable"
	if experimental {
		crdType = "experimental"
	}

	log.Printf("Installing %s CRDs", crdType)

	scheme := runtime.NewScheme()

	for _, fn := range schemes {
		utilruntime.Must(fn(scheme))
	}

	k8sClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		log.Fatalf("%s", fmt.Errorf("unable to install crds: %w", err))
	}

	toInstall := stableCRDs
	if experimental {
		toInstall = append(toInstall, experimentalCRDs...)
	}

	var errs []error
	for _, crd := range toInstall {
		errs = append(errs, ensureCRD(ctx, k8sClient, crd))
	}

	if err := errors.Join(errs...); err != nil {
		log.Fatalf("%s", fmt.Errorf("issues while installing crds: %w", err))
	}
}

func ensureCRD(ctx context.Context, k8sClient client.Client, crd *apiextensionsv1.CustomResourceDefinition) error {
	var existing apiextensionsv1.CustomResourceDefinition
	existing.Name = crd.Name
	_, err := controllerutil.CreateOrUpdate(ctx, k8sClient, &existing, func() error {
		existing.Annotations = crd.Annotations
		existing.Labels = crd.Labels
		existing.Spec = crd.Spec
		return nil
	})
	return err
}
