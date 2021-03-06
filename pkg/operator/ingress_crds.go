package operator

import (
	"context"
	"reflect"

	etx "github.com/appscode/go/context"
	"github.com/appscode/go/log"
	tools "github.com/appscode/kutil/tools/monitoring"
	"github.com/appscode/kutil/tools/monitoring/agents"
	api "github.com/appscode/voyager/apis/voyager/v1beta1"
	"github.com/appscode/voyager/pkg/eventer"
	"github.com/appscode/voyager/pkg/ingress"
	"github.com/google/go-cmp/cmp"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
)

// Blocks caller. Intended to be called as a Go routine.
func (op *Operator) initIngressCRDWatcher() cache.Controller {
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return op.VoyagerClient.Ingresses(op.Opt.WatchNamespace()).List(metav1.ListOptions{})
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return op.VoyagerClient.Ingresses(op.Opt.WatchNamespace()).Watch(metav1.ListOptions{})
		},
	}
	_, informer := cache.NewInformer(lw,
		&api.Ingress{},
		op.Opt.ResyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				ctx := etx.Background()
				logger := log.New(ctx)
				if engress, ok := obj.(*api.Ingress); ok {
					engress.Migrate()
					logger.Infof("%s %s@%s added", engress.APISchema(), engress.Name, engress.Namespace)
					if !engress.ShouldHandleIngress(op.Opt.IngressClass) {
						logger.Infof("%s %s@%s does not match ingress class", engress.APISchema(), engress.Name, engress.Namespace)
						return
					}
					if err := engress.IsValid(op.Opt.CloudProvider); err != nil {
						op.recorder.Eventf(
							engress.ObjectReference(),
							core.EventTypeWarning,
							eventer.EventReasonIngressInvalid,
							"Reason: %s",
							err.Error(),
						)
						return
					}

					op.AddEngress(ctx, engress)
				}
			},
			UpdateFunc: func(old, new interface{}) {
				ctx := etx.Background()
				logger := log.New(ctx)
				oldEngress, ok := old.(*api.Ingress)
				if !ok {
					logger.Errorln("Invalid Ingress object")
					return
				}
				oldEngress.Migrate()
				newEngress, ok := new.(*api.Ingress)
				if !ok {
					logger.Errorln("Invalid Ingress object")
					return
				}
				newEngress.Migrate()
				if changed, _ := oldEngress.HasChanged(*newEngress); !changed {
					return
				}
				diff := cmp.Diff(oldEngress, newEngress,
					cmp.Comparer(func(x, y resource.Quantity) bool {
						return x.Cmp(y) == 0
					}),
					cmp.Comparer(func(x, y *metav1.Time) bool {
						if x == nil && y == nil {
							return true
						}
						if x != nil && y != nil {
							return x.Time.Equal(y.Time)
						}
						return false
					}))
				logger.Infof("%s %s@%s has changed. Diff: %s", newEngress.APISchema(), newEngress.Name, newEngress.Namespace, diff)
				if err := newEngress.IsValid(op.Opt.CloudProvider); err != nil {
					op.recorder.Eventf(
						newEngress.ObjectReference(),
						core.EventTypeWarning,
						eventer.EventReasonIngressInvalid,
						"Reason: %s",
						err.Error(),
					)
					return
				}
				op.UpdateEngress(ctx, oldEngress, newEngress)
			},
			DeleteFunc: func(obj interface{}) {
				if engress, ok := obj.(*api.Ingress); ok {
					engress.Migrate()
					ctx := etx.Background()
					logger := log.New(ctx)
					logger.Infof("%s %s@%s deleted", engress.APISchema(), engress.Name, engress.Namespace)
					if !engress.ShouldHandleIngress(op.Opt.IngressClass) {
						logger.Infof("%s %s@%s does not match ingress class", engress.APISchema(), engress.Name, engress.Namespace)
						return
					}
					op.DeleteEngress(ctx, engress)
				}
			},
		},
	)
	return informer
}

func (op *Operator) AddEngress(ctx context.Context, engress *api.Ingress) {
	ctrl := ingress.NewController(ctx, op.KubeClient, op.CRDClient, op.VoyagerClient, op.PromClient, op.ServiceLister, op.EndpointsLister, op.Opt, engress)
	if ctrl.IsExists() {
		if err := ctrl.Update(ingress.UpdateStats, nil); err != nil {
			log.Errorln(err)
		}
		return
	} else {
		ctrl.Create()
	}
}

func (op *Operator) UpdateEngress(ctx context.Context, oldEngress, newEngress *api.Ingress) {
	oldHandled := oldEngress.ShouldHandleIngress(op.Opt.IngressClass)
	newHandled := newEngress.ShouldHandleIngress(op.Opt.IngressClass)
	if !oldHandled && !newHandled {
		return
	}

	ctrl := ingress.NewController(ctx, op.KubeClient, op.CRDClient, op.VoyagerClient, op.PromClient, op.ServiceLister, op.EndpointsLister, op.Opt, newEngress)
	if oldHandled && !newHandled {
		ctrl.Delete()
	} else {
		if ctrl.IsExists() {
			var updateMode ingress.UpdateMode
			if oldEngress.IsStatsChanged(*newEngress) {
				updateMode |= ingress.UpdateStats
			}
			// Check for changes in ingress.appscode.com/monitoring-agent
			if newMonSpec, newErr := tools.Parse(newEngress.Annotations, api.EngressKey, api.DefaultExporterPortNumber); newErr == nil {
				if oldMonSpec, oldErr := tools.Parse(oldEngress.Annotations, api.EngressKey, api.DefaultExporterPortNumber); oldErr == nil {
					if !reflect.DeepEqual(oldMonSpec, newMonSpec) {
						agent := agents.New(newMonSpec.Agent, op.KubeClient, op.CRDClient, op.PromClient)
						err := agent.Update(newEngress.StatsAccessor(), oldMonSpec, newMonSpec)
						if err != nil {
							return
						}
					}
					if (oldMonSpec == nil && newMonSpec != nil) ||
						(oldMonSpec != nil && newMonSpec == nil) {
						updateMode |= ingress.UpdateStats
					}
				}
			}

			// For ingress update update HAProxy once
			ctrl.Update(updateMode, oldEngress)
		} else {
			ctrl.Create()
		}
	}

	backends := map[string]metav1.ObjectMeta{}
	for k, v := range oldEngress.BackendServices() {
		backends[k] = v
	}
	for k, v := range newEngress.BackendServices() {
		backends[k] = v
	}
}

func (op *Operator) DeleteEngress(ctx context.Context, engress *api.Ingress) {
	ctrl := ingress.NewController(ctx, op.KubeClient, op.CRDClient, op.VoyagerClient, op.PromClient, op.ServiceLister, op.EndpointsLister, op.Opt, engress)
	ctrl.Delete()
}
