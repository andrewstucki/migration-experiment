package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/redpanda-data/common-go/kube"
	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	crdscmd "github.com/andrewstucki/migration-experiment/cmds/crds"
	entrycmd "github.com/andrewstucki/migration-experiment/cmds/entry"
	"github.com/andrewstucki/migration-experiment/log"
	"github.com/andrewstucki/migration-experiment/reconcilers"
)

type Config struct {
	Service   string
	Namespace string
	Image     migrationv1alpha1.Image
}

func (c *Config) Validate() error {
	var err error
	if c.Service == "" {
		err = errors.Join(err, errors.New("service must be specified"))
	}
	if c.Namespace == "" {
		err = errors.Join(err, errors.New("namespace must be specified"))
	}
	return err
}

func main() {
	var config Config

	root := cobra.Command{
		Use:  "migration-operator",
		Long: "migration-operator is the main entrypoint for the Migration operator.",
		Run: func(cmd *cobra.Command, args []string) {
			if err := run(config); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
		},
	}

	root.AddCommand(crdscmd.Command())
	root.AddCommand(entrycmd.Command())

	root.Flags().StringVarP(&config.Image.Tag, "image.tag", "t", "", "Image tag to use for the operator")
	root.Flags().StringVarP(&config.Image.Repository, "image.repo", "r", "", "Image repository to use for the operator")
	root.Flags().StringVarP(&config.Service, "service", "s", "", "Service where this is deployed")
	root.Flags().StringVarP(&config.Namespace, "namespace", "n", "", "Namespace where this is deployed")

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions/status,verbs=update;patch

func run(config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}

	ctx := ctrl.SetupSignalHandler()
	rotator := kube.NewCertRotator(kube.CertRotatorConfig{
		DNSName: fmt.Sprintf("%s.%s.svc", config.Service, config.Namespace),
		SecretKey: types.NamespacedName{
			Namespace: config.Namespace,
			Name:      config.Service + "-certificate",
		},
		Service: &apiextensionsv1.ServiceReference{
			Namespace: config.Namespace,
			Name:      config.Service,
			Path:      ptr.To("/convert"),
		},
		Webhooks: []kube.WebhookInfo{{
			Type:     kube.CRDConversion,
			Name:     "olds.migration.lambda.coffee",
			Versions: []string{"v1alpha1", "v1beta1", "v1"},
		}, {
			Type:     kube.CRDConversion,
			Name:     "news.migration.lambda.coffee",
			Versions: []string{"v1alpha1", "v1beta1", "v1"},
		}},
	})

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(migrationv1alpha1.Install(scheme))

	restConfig := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			TLSOpts: []func(*tls.Config){func(c *tls.Config) {
				c.GetCertificate = rotator.GetCertificate
			}},
		}),
	})
	if err != nil {
		log.Error(err, "initializing manager")
		os.Exit(1)
	}

	if err := kube.AddRotator(mgr, rotator); err != nil {
		log.Error(err, "setting up cert rotator")
		os.Exit(1)
	}

	if err := ctrl.NewWebhookManagedBy(mgr).For(&migrationv1alpha1.Old{}).Complete(); err != nil {
		log.Error(err, "setting up old webhook")
		os.Exit(1)
	}
	if err := ctrl.NewWebhookManagedBy(mgr).For(&migrationv1alpha1.New{}).Complete(); err != nil {
		log.Error(err, "setting up new webhook")
		os.Exit(1)
	}

	if err := reconcilers.SetupOldReconciler(config.Image, mgr); err != nil {
		log.Error(err, "setting up old reconciler")
		os.Exit(1)
	}

	if err := reconcilers.SetupNewReconciler(config.Image, mgr); err != nil {
		log.Error(err, "setting up new reconciler")
		os.Exit(1)
	}

	log.Info("starting manager")
	return mgr.Start(ctx)
}
