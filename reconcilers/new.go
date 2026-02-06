package reconcilers

import (
	"context"

	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	migrationv1alpha1 "github.com/andrewstucki/migration-experiment/apis/migration/v1alpha1"
	"github.com/andrewstucki/migration-experiment/render"
)

type NewReconciler struct {
	ctl           *kube.Ctl
	operator      migrationv1alpha1.Image
	migrator      render.OldToNewStatefulMigrator
	manager       *PodManager
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

	source, err := r.migrator.EnsureMigrated(ctx, object)
	if err != nil {
		return ctrl.Result{}, err
	}
	if source != nil {
		logger.Info("migrated from old to new", "source", source.Name)
	}

	if _, err := syncer.Sync(ctx); err != nil {
		return ctrl.Result{}, err
	}

	var sets appsv1.StatefulSetList
	err = r.ctl.List(ctx, object.GetNamespace(), &sets, client.MatchingLabels(render.NewOwnershipLabels(object)))
	if err != nil {
		return ctrl.Result{}, err
	}

	// ensure we're all up-to-date
	for _, set := range sets.Items {
		outdated, err := r.manager.GetNextOutdatedPod(ctx, &set)
		if err != nil {
			return ctrl.Result{}, err
		}
		if outdated != nil {
			logger.Info("found outdated pod, rolling", "pod", outdated.Name)
			if err := r.ctl.Delete(ctx, outdated); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	desired := render.NewStatefulSet(r.operator, object)
	if err := r.ctl.Apply(ctx, desired, client.ForceOwnership); err != nil {
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
		For(&migrationv1alpha1.New{}).
		Owns(&appsv1.StatefulSet{})

	maybeWatchResources(ctl.Scheme(), ctl.RESTMapper(), builder, render.NewRenderedTypes(), render.NewNameLabelKey, render.NewNamespaceLabelKey)

	return builder.Complete(&NewReconciler{operator: operator, ctl: ctl, syncerFactory: render.NewSyncerFactoryForKubeCtl(ctl), migrator: render.NewOldToNewStatefulMigrator(ctl), manager: NewPodManager(ctl)})
}
