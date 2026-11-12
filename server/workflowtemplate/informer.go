package workflowtemplate

import (
	"context"
	"os"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	wfextvv1alpha1 "github.com/argoproj/argo-workflows/v4/pkg/client/informers/externalversions/workflow/v1alpha1"
	"github.com/argoproj/argo-workflows/v4/server/types"
	"github.com/argoproj/argo-workflows/v4/util/logging"
	"github.com/argoproj/argo-workflows/v4/util/namespaces"
	"github.com/argoproj/argo-workflows/v4/workflow/controller/informer"
	"github.com/argoproj/argo-workflows/v4/workflow/templateresolution"
)

const (
	workflowTemplateResyncPeriod = 20 * time.Minute
)

var _ types.WorkflowTemplateStore = &Informer{}

type Informer struct {
	managedNamespace string
	informer         wfextvv1alpha1.WorkflowTemplateInformer
}

func NewInformer(restConfig *rest.Config, managedNamespace string) (*Informer, error) {
	dynamicInterface, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}
	informer := informer.NewTolerantWorkflowTemplateInformer(
		dynamicInterface,
		workflowTemplateResyncPeriod,
		managedNamespace)
	return &Informer{
		informer:         informer,
		managedNamespace: managedNamespace,
	}, nil
}

// Run starts the informer in a separate go-routine and blocks until cache sync.
func (wti *Informer) Run(ctx context.Context, stopCh <-chan struct{}) {
	go wti.informer.Informer().Run(stopCh)

	if !cache.WaitForCacheSync(
		stopCh,
		wti.informer.Informer().HasSynced,
	) {
		logging.RequireLoggerFromContext(ctx).Error(ctx, "Timed out waiting for caches to sync")
		os.Exit(1)
	}
}

// Getter returns a WorkflowTemplateNamespacedGetter. If namespace is empty, the Lister
// will use the first namespace provided during initialization (an allowlist has no
// single fallback; requests for a multi-namespace install are expected to carry their
// namespace).
func (wti *Informer) Getter(ctx context.Context, namespace string) templateresolution.WorkflowTemplateNamespacedGetter {
	if wti.informer == nil {
		logging.RequireLoggerFromContext(ctx).Error(ctx, "Template informer not started")
		os.Exit(1)
	}
	if namespace == "" {
		if managed := namespaces.Parse(wti.managedNamespace); len(managed) != 0 {
			namespace = managed[0]
		}
	}
	return templateresolution.WrapWorkflowTemplateLister(wti.informer.Lister().WorkflowTemplates(namespace))
}
