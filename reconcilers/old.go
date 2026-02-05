package reconcilers

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	"github.com/andrewstucki/migration-experiment/render"
)

type OldReconciler struct {
	ctl           *kube.Ctl
	operator      migrationv1alpha1.Image
	syncerFactory render.SyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old]
}

// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=olds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=olds/status,verbs=update;patch
// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=olds/finalizers,verbs=update

func (r *OldReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("reconciling old")

	object := new(migrationv1alpha1.Old)
	err := r.ctl.Get(ctx, req.NamespacedName, object)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	syncer := r.syncerFactory.Syncer(object)

	if object.GetDeletionTimestamp() != nil {
		if controllerutil.RemoveFinalizer(object, Finalizer) {

			if _, err := syncer.DeleteAll(ctx); err != nil {
				return ctrl.Result{}, err
			}

			if err := r.ctl.Update(ctx, object); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if controllerutil.AddFinalizer(object, Finalizer) {
		if err := r.ctl.Apply(ctx, object); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if _, err := syncer.Sync(ctx); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func SetupOldReconciler(operator migrationv1alpha1.Image, mgr ctrl.Manager) error {
	ctl, err := kube.FromRESTConfig(mgr.GetConfig(), kube.Options{
		Options: client.Options{
			Scheme: mgr.GetScheme(),
			Mapper: mgr.GetRESTMapper(),
			Cache: &client.CacheOptions{
				Reader: mgr.GetCache(),
			},
		},
	})
	if err != nil {
		return err
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&migrationv1alpha1.Old{})

	maybeWatchResources(ctl.Scheme(), ctl.RESTMapper(), builder, render.OldRenderedTypes(), render.OldNameLabelKey, render.OldNamespaceLabelKey)

	return builder.Complete(&OldReconciler{operator: operator, ctl: ctl, syncerFactory: render.OldSyncerFactoryForKubeCtl(ctl)})
}
