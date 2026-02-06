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
	migrationkube "github.com/andrewstucki/migration-experiment/kube"
	"github.com/andrewstucki/migration-experiment/render"
)

type OldReconciler struct {
	ctl           *kube.Ctl
	operator      migrationv1alpha1.Image
	migrator      render.OldToNewStatefulMigrator
	manager       *migrationkube.PodManager
	scaler        *migrationkube.StatefulSetScaler
	syncerFactory migrationkube.SyncerFactory[migrationv1alpha1.Old, *migrationv1alpha1.Old]
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

	var existingSets appsv1.StatefulSetList
	err = r.ctl.List(ctx, object.GetNamespace(), &existingSets, client.MatchingLabels(render.OldOwnershipLabels(object)))
	if err != nil {
		return ctrl.Result{}, err
	}

	if object.GetDeletionTimestamp() != nil {
		if controllerutil.RemoveFinalizer(object, Finalizer) {
			if _, err := syncer.DeleteAll(ctx); err != nil {
				return ctrl.Result{}, err
			}

			for _, set := range existingSets.Items {
				if err := r.ctl.Delete(ctx, &set); err != nil {
					return ctrl.Result{}, err
				}
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

	existing, err := kube.Items[*appsv1.StatefulSet](&existingSets)
	if err != nil {
		return ctrl.Result{}, err
	}

	if controllerutil.AddFinalizer(object, Finalizer) {
		if err := r.ctl.Apply(ctx, object); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if r.migrator.ShouldMigrateSource(object) {
		logger.Info("object should be migrated, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	if _, err := syncer.Sync(ctx); err != nil {
		return ctrl.Result{}, err
	}

	desired := render.OldStatefulSets(r.operator, object)
	for _, set := range desired {
		if err := controllerutil.SetControllerReference(object, set, r.ctl.Scheme()); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.ctl.Apply(ctx, set, client.ForceOwnership); err != nil {
			return ctrl.Result{}, err
		}
	}

	// ensure we're all up-to-date
	stable, err := r.manager.AllPodsStable(ctx, append(existing, desired...))
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, set := range existing {
		outdated, err := r.manager.GetNextOutdatedPod(ctx, set)
		if err != nil {
			return ctrl.Result{}, err
		}
		if outdated != nil && stable {
			logger.Info("found outdated pod, rolling", "pod", outdated.Name)
			if err := r.ctl.Delete(ctx, outdated); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
	}

	if stable {
		scaledDown, err := r.scaler.ScaleDownFirstUndesired(ctx, existing, desired)
		if err != nil {
			return ctrl.Result{}, err
		}
		if scaledDown != nil {
			logger.Info("scaled down set", "set", scaledDown.Name)
		}
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
		For(&migrationv1alpha1.Old{}).
		Owns(&appsv1.StatefulSet{})

	maybeWatchResources(ctl.Scheme(), ctl.RESTMapper(), builder, render.OldRenderedTypes(), render.OldNameLabelKey, render.OldNamespaceLabelKey)

	return builder.Complete(&OldReconciler{operator: operator, ctl: ctl, syncerFactory: render.OldSyncerFactoryForKubeCtl(ctl), migrator: render.NewOldToNewStatefulMigrator(ctl), manager: migrationkube.NewPodManager(ctl), scaler: migrationkube.NewStatefulSetScaler(ctl)})
}
