package reconciler

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	tektonversioned2 "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"

	// "knative.dev/pkg/ptr"
	"knative.dev/pkg/reconciler"
	kueueversioned "sigs.k8s.io/kueue/client-go/clientset/versioned"
	kueuev1beta1lister "sigs.k8s.io/kueue/client-go/listers/kueue/v1beta1"
)

const (
	groupName     = "pipelinesascode.tekton.dev"
	gitAuthSecret = groupName + "/git-auth-secret"
)

// Reconciler implements controller.Reconciler for Workload resources.
type Reconciler struct {
	logger         *zap.SugaredLogger
	hubKubeClient  kubernetes.Interface
	workloadLister kueuev1beta1lister.WorkloadLister
	kueueClient    kueueversioned.Interface
	kueueNamespace string
}

var (
	_ controller.Reconciler  = (*Reconciler)(nil)
	_ reconciler.LeaderAware = (*Reconciler)(nil)
)

// Promote implements reconciler.LeaderAware
func (r *Reconciler) Promote(b reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
	// Return nil to indicate we don't need special promotion logic
	return nil
}

// Demote implements reconciler.LeaderAware
func (r *Reconciler) Demote(b reconciler.Bucket) {
	// Nothing to do on demotion
}

// Reconcile is the main entry point for reconciling Workload resources.
// This function is called only for Workloads that have a PipelineRun owner reference.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	// Parse the key
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logger.Errorf("invalid resource key: %s", key)
		return nil
	}

	logger = logger.With("namespace", namespace, "workload", name)
	logger.Debugf("reconciling workload %s/%s", namespace, name)

	workload, err := r.workloadLister.Workloads(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Debugf("workload %s/%s no longer exists, may be deleted, skipping reconciliation", namespace, name)
			return nil
		}
		logger.Errorf("error getting workload %s/%s: %v", namespace, name, err)
		return err
	}

	if workload.Spec.Active != nil && !*workload.Spec.Active {
		logger.Infof("workload %s/%s is not active, skipping reconciliation", namespace, name)
		return nil
	}

	if workload.Status.ClusterName == nil || *workload.Status.ClusterName == "" {
		logger.Infof("workload %s/%s has no cluster name, skipping reconciliation", namespace, name)
		return nil
	}

	ownerPipelineRunReference := metav1.GetControllerOf(workload)

	if ownerPipelineRunReference == nil {
		logger.Infof("workload %s/%s has no owner PipelineRun, skipping reconciliation", namespace, name)
		return nil
	}

	if ownerPipelineRunReference.Kind != "PipelineRun" {
		logger.Infof("workload %s/%s has owner reference of kind %s, skipping reconciliation", namespace, name, ownerPipelineRunReference.Kind)
		return nil
	}

	if workload.Status.ClusterName != nil {
		logger = logger.With("clusterInfo", workload.Status.ClusterName)
	}

	logger = logger.With("PipelineRun", ownerPipelineRunReference.Name)

	spokeClusterConfig, err := r.getSpokeClusterConfig(ctx, *workload.Status.ClusterName)
	if err != nil {
		r.logger.Errorf("error getting spoke cluster config for workload %s/%s: %v", workload.GetNamespace(), workload.GetName(), err)
		return err
	}

	spokeKubeClient, err := kubernetes.NewForConfig(spokeClusterConfig)
	if err != nil {
		r.logger.Errorf("error creating spoke kube client: %v", err)
		return err
	}

	spokeTektonClient, err := tektonversioned2.NewForConfig(spokeClusterConfig)
	if err != nil {
		r.logger.Errorf("error creating spoke tekton client for workload %s/%s: %v", workload.GetNamespace(), workload.GetName(), err)
		return err
	}

	secretName, pipelineRun, err := r.validatePLRAndGetSecretName(ctx, spokeTektonClient, ownerPipelineRunReference.Name, workload.GetNamespace(), *workload.Status.ClusterName)
	if err != nil {
		return err
	}

	if secretName == "" {
		return nil
	}

	err = r.syncSecretToSpokeCluster(ctx, secretName, *workload.Status.ClusterName, spokeKubeClient, pipelineRun)
	if err != nil {
		logger.Errorf("error syncing secret %s/%s to spoke cluster %s: %v", pipelineRun.GetNamespace(), secretName, *workload.Status.ClusterName, err)
		return err
	}

	logger.Infof("successfully reconciled workload %s/%s owned by PipelineRun %s",
		workload.GetNamespace(), workload.GetName(), pipelineRun.GetName())
	return nil
}

func (r *Reconciler) validatePLRAndGetSecretName(ctx context.Context, spokeTektonClient tektonversioned2.Interface, plrName, plrNamespace, clusterName string) (string, *v1.PipelineRun, error) {
	pipelineRun, err := spokeTektonClient.TektonV1().PipelineRuns(plrNamespace).Get(ctx, plrName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			r.logger.Infof("PipelineRun %s/%s is not created yet on spoke cluster %s, skipping reconciliation: %v", plrNamespace, plrName, clusterName, err)
			return "", nil, nil
		}
		r.logger.Errorf("error getting PipelineRun %s/%s on spoke cluster %s: %v", plrNamespace, plrName, clusterName, err)
		return "", nil, err
	}

	r.logger.Infof("retrieved PipelineRun %s/%s successfully from spoke cluster %s", plrNamespace, plrName, clusterName)

	if pipelineRun.IsDone() {
		r.logger.Infof("PipelineRun %s/%s is done on spoke cluster %s, skipping reconciliation", plrNamespace, plrName, clusterName)
		return "", nil, nil
	}

	secretName, ok := pipelineRun.GetAnnotations()[gitAuthSecret]
	if !ok {
		r.logger.Infof("git auth secret not found for PipelineRun %s/%s on spoke cluster %s", plrNamespace, plrName, clusterName)
		return "", nil, nil
	}

	r.logger.Infof("PipelineRun %s/%s has git auth secret %s", plrNamespace, plrName, secretName)

	return secretName, pipelineRun, nil
}

func (r *Reconciler) syncSecretToSpokeCluster(ctx context.Context, secretName string, clusterName string, spokeKubeClient kubernetes.Interface, pipelineRun *v1.PipelineRun) error {
	secret, err := r.hubKubeClient.CoreV1().Secrets(pipelineRun.GetNamespace()).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		r.logger.Errorf("error getting secret %s/%s for PipelineRun %s: %v", pipelineRun.GetNamespace(), secretName, pipelineRun.GetName(), err)
		return err
	}

	r.logger.Infof("retrieved secret %s/%s for PipelineRun %s successfully", pipelineRun.GetNamespace(), secretName, pipelineRun.GetName())

	// Create a new secret object with only the required fields
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secret.Name,
			Namespace:   secret.Namespace,
			Labels:      secret.Labels,
			Annotations: secret.Annotations,
		},
		Type: secret.Type,
		Data: secret.Data,
	}

	// Copy owner references if they exist
	if len(secret.OwnerReferences) > 0 {
		newSecret.OwnerReferences = make([]metav1.OwnerReference, len(secret.OwnerReferences))
		for i, ref := range secret.OwnerReferences {
			newSecret.OwnerReferences[i] = ref
			// Override only the UID to point to the spoke cluster's PipelineRun
			newSecret.OwnerReferences[i].UID = pipelineRun.GetUID()
		}
	}

	secrets := spokeKubeClient.CoreV1().Secrets(newSecret.Namespace)
	if _, err := secrets.Create(ctx, newSecret, metav1.CreateOptions{}); err == nil {
		r.logger.Infof("successfully created secret %s/%s on spoke cluster %s", newSecret.Namespace, newSecret.Name, clusterName)
		return nil
	} else if !errors.IsAlreadyExists(err) {
		r.logger.Errorf("error creating secret %s/%s: %v", newSecret.Namespace, newSecret.Name, err)
		return err
	}

	existing, err := secrets.Get(ctx, newSecret.Name, metav1.GetOptions{})
	if err != nil {
		r.logger.Errorf("error getting existing secret %s/%s: %v", newSecret.Namespace, newSecret.Name, err)
		return err
	}
	if existing.Type == newSecret.Type && equality.Semantic.DeepEqual(existing.Data, newSecret.Data) {
		r.logger.Infof("secret %s/%s is already up to date on spoke cluster %s", newSecret.Namespace, newSecret.Name, clusterName)
		return nil
	}

	// The hub owns the Secret payload; preserve spoke metadata and resourceVersion.
	updated := existing.DeepCopy()
	updated.Type = newSecret.Type
	updated.Data = newSecret.Data
	if _, err := secrets.Update(ctx, updated, metav1.UpdateOptions{}); err != nil {
		r.logger.Errorf("error updating secret %s/%s: %v", newSecret.Namespace, newSecret.Name, err)
		return err
	}

	r.logger.Infof("successfully updated secret %s/%s on spoke cluster %s", newSecret.Namespace, newSecret.Name, clusterName)
	return nil
}

// getSpokeClusterConfig retrieves the REST config for a spoke cluster.
func (r *Reconciler) getSpokeClusterConfig(ctx context.Context, clusterName string) (*rest.Config, error) {
	mkCluster, err := r.kueueClient.KueueV1beta1().MultiKueueClusters().Get(ctx, clusterName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not find MultiKueueCluster %s: %w", clusterName, err)
	}

	kubeConfig := mkCluster.Spec.KubeConfig

	switch kubeConfig.LocationType {
	case "Secret":
		kubeconfigSecret, err := r.hubKubeClient.CoreV1().Secrets(r.kueueNamespace).Get(ctx, kubeConfig.Location, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("could not get kubeconfig secret %s/%s: %w", r.kueueNamespace, kubeConfig.Location, err)
		}

		kubeconfigBytes, ok := kubeconfigSecret.Data["kubeconfig"]
		if !ok {
			return nil, fmt.Errorf("kubeconfig secret %s/%s is missing 'kubeconfig' data key", r.kueueNamespace, kubeConfig.Location)
		}

		return clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	case "Path":
		return clientcmd.BuildConfigFromFlags("", kubeConfig.Location)
	default:
		return nil, fmt.Errorf("unsupported kubeconfig location type: %s", kubeConfig.LocationType)
	}
}
