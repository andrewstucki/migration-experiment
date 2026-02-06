package render

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/redpanda-data/common-go/kube"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TODO: extract to kube package

var (
	ErrMigrationSourceNotFound  = errors.New("migration source not found")
	ErrUnmatchedMigrationTarget = errors.New("migration target and source both require migration labels")
)

func ptrFor[T any, PT PObject[T]]() PT {
	var v T
	return &v
}

type StatefulMigrator[T any, U any, PT PObject[T], PU PObject[U]] interface {
	EnsureMigrated(context.Context, PU) (PT, error)
	ShouldMigrateSource(PT) bool
	ShouldMigrateTarget(PU) bool
}

type Migrator[T any, U any, PT PObject[T], PU PObject[U]] interface {
	SourceFromTarget(PU) *types.NamespacedName
	MarkMigrated(PT)
	IsMigrated(PT) bool
	IsMigrating(PT) bool
}

func NewStatefulMigrator[T any, U any, PT PObject[T], PU PObject[U]](
	ctl *kube.Ctl,
	migrator Migrator[T, U, PT, PU],
	sourceSyncerFactory SyncerFactory[T, PT],
	targetSyncerFactory SyncerFactory[U, PU],
) StatefulMigrator[T, U, PT, PU] {
	return &statefulMigrator[T, U, PT, PU]{
		ctl:                 ctl,
		migrator:            migrator,
		sourceSyncerFactory: sourceSyncerFactory,
		targetSyncerFactory: targetSyncerFactory,
	}
}

type statefulMigrator[T any, U any, PT PObject[T], PU PObject[U]] struct {
	ctl                 *kube.Ctl
	migrator            Migrator[T, U, PT, PU]
	sourceSyncerFactory SyncerFactory[T, PT]
	targetSyncerFactory SyncerFactory[U, PU]
}

func (m *statefulMigrator[T, U, PT, PU]) getMigrationSource(ctx context.Context, target PU) (PT, error) {
	if namespacedName := m.migrator.SourceFromTarget(target); namespacedName != nil {
		source := ptrFor[T, PT]()
		err := m.ctl.Get(ctx, *namespacedName, source)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return source, nil
	}
	return nil, nil
}

func (m *statefulMigrator[T, U, PT, PU]) EnsureMigrated(ctx context.Context, target PU) (PT, error) {
	source, err := m.getMigrationSource(ctx, target)
	if err != nil {
		return nil, err
	}

	// if we can't find a source either error or no-op depending on whether or not this target should be migrated
	if source == nil {
		if m.ShouldMigrateTarget(target) {
			return nil, ErrMigrationSourceNotFound
		}
		return nil, nil
	}

	// at this point we know we have a source, so check to make sure we're supposed to be migrating it
	if !m.ShouldMigrateSource(source) {
		return nil, ErrUnmatchedMigrationTarget
	}

	// if the source is already marked as migrated, we can skip everything else
	if m.migrator.IsMigrated(source) {
		return source, nil
	}

	targetSyncer := m.targetSyncerFactory.Syncer(target)
	targetRendered, err := targetSyncer.Sync(ctx)
	if err != nil {
		return nil, err
	}
	remainder, err := m.findRemainder(ctx, source, targetRendered)
	if err != nil {
		return nil, err
	}

	if err := m.adoptStatefulSets(ctx, source, target); err != nil {
		return nil, err
	}

	migrated, err := m.checkMigrated(ctx, source, target)
	if err != nil {
		return nil, err
	}

	if migrated {
		for _, obj := range remainder {
			// clean up all of our source objects that we don't need anymore
			if err := m.ctl.Delete(ctx, obj); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}
		}

		if err := retry.RetryOnConflict(wait.Backoff{
			Duration: 10 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.1,
			Steps:    3,
		}, func() error {
			if err := m.ctl.Get(ctx, client.ObjectKeyFromObject(source), source); err != nil {
				return err
			}

			// mark the source as migrated
			m.migrator.MarkMigrated(source)
			// and update to ensure we've persisted the migration marker
			return m.ctl.Update(ctx, source)
		}); err != nil {
			return nil, err
		}
	}

	return source, nil
}

func (m *statefulMigrator[T, U, PT, PU]) adoptStatefulSets(ctx context.Context, source PT, target PU) error {
	sourceLabels := m.sourceSyncerFactory.Syncer(source).OwnerLabels()

	var sets appsv1.StatefulSetList
	if err := m.ctl.List(ctx, source.GetNamespace(), &sets, client.MatchingLabels(sourceLabels)); err != nil {
		return err
	}
	for _, set := range sets.Items {
		if err := retry.RetryOnConflict(wait.Backoff{
			Duration: 10 * time.Millisecond,
			Factor:   1.5,
			Jitter:   0.1,
			Steps:    3,
		}, func() error {
			return m.updateStatefulSet(ctx, source, target, set.DeepCopy())
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *statefulMigrator[T, U, PT, PU]) updateStatefulSet(ctx context.Context, source PT, target PU, set *appsv1.StatefulSet) error {
	if err := m.ctl.Get(ctx, client.ObjectKeyFromObject(set), set); err != nil {
		return err
	}

	sourceLabels := m.sourceSyncerFactory.Syncer(source).OwnerLabels()
	targetLabels := m.targetSyncerFactory.Syncer(target).OwnerLabels()

	labels := set.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	maps.DeleteFunc(labels, func(k, v string) bool { return sourceLabels[k] == v })
	maps.Copy(labels, targetLabels)
	set.SetLabels(labels)

	if err := controllerutil.RemoveControllerReference(source, set, m.ctl.Scheme()); err != nil {
		return err
	}
	if err := controllerutil.SetControllerReference(target, set, m.ctl.Scheme()); err != nil {
		return err
	}
	return m.ctl.Update(ctx, set)
}

func (m *statefulMigrator[T, U, PT, PU]) findRemainder(ctx context.Context, source PT, rendered []kube.Object) ([]kube.Object, error) {
	sourceSyncer := m.sourceSyncerFactory.Syncer(source)

	sourceSet := map[schema.GroupKind]map[types.NamespacedName]struct{}{}
	items, err := sourceSyncer.ListInPurview(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range items {
		gvk, err := kube.GVKFor(m.ctl.Scheme(), o)
		if err != nil {
			return nil, err
		}
		gk := gvk.GroupKind()
		if _, ok := sourceSet[gk]; !ok {
			sourceSet[gk] = map[types.NamespacedName]struct{}{}
		}
		sourceSet[gk][types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}] = struct{}{}
	}

	remainder := []kube.Object{}
	for _, o := range rendered {
		gvk, err := kube.GVKFor(m.ctl.Scheme(), o)
		if err != nil {
			return nil, err
		}
		gk := gvk.GroupKind()
		if _, ok := sourceSet[gk]; !ok {
			remainder = append(remainder, o)
			continue
		}
		if _, ok := sourceSet[gk][types.NamespacedName{Namespace: o.GetNamespace(), Name: o.GetName()}]; !ok {
			remainder = append(remainder, o)
			continue
		}
	}

	return remainder, nil
}

func (m *statefulMigrator[T, U, PT, PU]) checkMigrated(ctx context.Context, source PT, target PU) (bool, error) {
	sourceSyncer := m.sourceSyncerFactory.Syncer(source)
	targetSyncer := m.targetSyncerFactory.Syncer(target)

	var sourceSets appsv1.StatefulSetList
	err := m.ctl.List(ctx, source.GetNamespace(), &sourceSets, client.MatchingLabels(sourceSyncer.OwnerLabels()))
	if err != nil {
		return false, err
	}

	// check to make sure we have no more source statefulsets
	if len(sourceSets.Items) > 0 {
		return false, nil
	}

	var targetSets appsv1.StatefulSetList
	err = m.ctl.List(ctx, target.GetNamespace(), &targetSets, client.MatchingLabels(targetSyncer.OwnerLabels()))
	if err != nil {
		return false, err
	}

	// ensure all target statefulsets are updated
	for _, set := range targetSets.Items {
		updated := set.Status.ObservedGeneration == set.Generation && set.Status.UpdatedReplicas == set.Status.Replicas
		if !updated {
			return false, nil
		}
	}

	return true, nil
}

func (m *statefulMigrator[T, U, PT, PU]) ShouldMigrateSource(source PT) bool {
	return m.migrator.IsMigrating(source) || m.migrator.IsMigrated(source)
}

func (m *statefulMigrator[T, U, PT, PU]) ShouldMigrateTarget(target PU) bool {
	return m.migrator.SourceFromTarget(target) != nil
}
