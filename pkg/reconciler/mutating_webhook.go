// Package reconciler implements routines to reconcile Kubernetes resources, currently limited to OSM's
// mutating webhook configuration.
package reconciler

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openservicemesh/osm/pkg/certificate/providers"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/errcode"
	"github.com/openservicemesh/osm/pkg/injector"
)

// MutatingWebhookConfigurationReconciler reconciles a MutatingWebhookConfiguration object
// TODO: Deprecate as a part of #4065
type MutatingWebhookConfigurationReconciler struct {
	runtimeclient.Client
	KubeClient   *kubernetes.Clientset
	Scheme       *runtime.Scheme
	OsmWebhook   string
	OsmNamespace string
}

// Reconcile is the reconciliation method for OSM MutatingWebhookConfiguration.
func (r *MutatingWebhookConfigurationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// reconcile only for OSM mutatingWebhookConfiguration
	if req.Name == r.OsmWebhook {
		instance := &v1beta1.MutatingWebhookConfiguration{}

		if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
			log.Error().Err(err).Msgf("Error reading object %s ", req.NamespacedName)
			return ctrl.Result{}, runtimeclient.IgnoreNotFound(err)
		}

		var shouldUpdate bool
		if r.OsmWebhook == instance.Name {
			//check if CA bundle exists on webhook
			for idx, webhook := range instance.Webhooks {
				// CA bundle missing for webhook, update webhook to include the latest CA bundle
				if webhook.Name == injector.MutatingWebhookName && webhook.ClientConfig.CABundle == nil {
					log.Trace().Msgf("CA bundle missing for webhook : %s ", req.Name)
					shouldUpdate = true
					webhookHandlerCert, err := providers.GetCertFromKubernetes(r.OsmNamespace, constants.WebhookCertificateSecretName, r.KubeClient)
					if err != nil {
						return ctrl.Result{}, errors.Errorf("Error fetching injector webhook certificate from k8s secret: %s", err)
					}
					instance.Webhooks[idx].ClientConfig.CABundle = webhookHandlerCert.GetCertificateChain()
				}
			}
		}

		if !shouldUpdate {
			log.Trace().Msgf("Mutatingwebhookconfiguration %s already compliant", req.Name)
			return ctrl.Result{}, nil
		}

		if err := r.Update(ctx, instance); err != nil {
			// TODO(#3962): metric might not be scraped before process restart resulting from this error
			log.Error().Err(err).Str(errcode.Kind, errcode.GetErrCodeWithMetric(errcode.ErrUpdatingMutatingWebhookCABundle)).
				Msgf("Error updating MutatingWebhookConfiguration %s", req.Name)
			return ctrl.Result{}, runtimeclient.IgnoreNotFound(err)
		}

		log.Debug().Msgf("Successfully updated CA Bundle for MutatingWebhookConfiguration %s ", req.Name)

		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager links the reconciler to the manager.
func (r *MutatingWebhookConfigurationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.MutatingWebhookConfiguration{}).
		Complete(r)
}