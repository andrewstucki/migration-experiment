package reconcilers

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	"github.com/andrewstucki/migration-experiment/render"
)

type NewReconciler struct {
	ctl           *kube.Ctl
	operator      migrationv1alpha1.Image
	syncerFactory render.SyncerFactory[migrationv1alpha1.New, *migrationv1alpha1.New]
}

// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=news,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=news/status,verbs=update;patch
// +kubebuilder:rbac:groups=migration.lambda.coffee,resources=news/finalizers,verbs=update

func (r *NewReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("reconciling new")

	object := new(migrationv1alpha1.New)
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

func SetupNewReconciler(operator migrationv1alpha1.Image, mgr ctrl.Manager) error {
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
		For(&migrationv1alpha1.New{})

	for _, o := range render.NewRenderedTypes() {
		builder.Watches(o, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			name, ok := obj.GetLabels()[render.NewNameLabelKey]
			if !ok {
				return nil
			}
			namespace, ok := obj.GetLabels()[render.NewNamespaceLabelKey]
			if !ok {
				return nil
			}
			return []ctrl.Request{{
				NamespacedName: types.NamespacedName{Namespace: namespace, Name: name},
			}}
		}))
	}

	return builder.Complete(&NewReconciler{operator: operator, ctl: ctl, syncerFactory: render.NewSyncerFactoryForKubeCtl(ctl)})
}
