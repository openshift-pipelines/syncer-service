package reconciler

import (
	"context"
	"fmt"
	"testing"

	v1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"go.uber.org/zap"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestSyncSecretToSpokeCluster(t *testing.T) {
	const (
		namespace  = "test-namespace"
		secretName = "git-auth"
	)

	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   namespace,
			Labels:      map[string]string{"source": "hub"},
			Annotations: map[string]string{"source": "hub"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "tekton.dev/v1",
				Kind:       "PipelineRun",
				Name:       "test-pipeline-run",
				UID:        "hub-pipeline-run",
			}},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"token": []byte("fresh")},
	}
	pipelineRun := &v1.PipelineRun{ObjectMeta: metav1.ObjectMeta{
		Name:      "test-pipeline-run",
		Namespace: namespace,
		UID:       "spoke-pipeline-run",
	}}
	existingSecret := func(data string, secretType corev1.SecretType) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            secretName,
				Namespace:       namespace,
				ResourceVersion: "7",
				Labels:          map[string]string{"source": "spoke"},
				Annotations:     map[string]string{"source": "spoke"},
				Finalizers:      []string{"spoke.example/finalizer"},
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       "spoke-owner",
					UID:        "spoke-owner",
				}},
			},
			Type: secretType,
			Data: map[string][]byte{"token": []byte(data)},
		}
	}

	tests := []struct {
		name         string
		existing     *corev1.Secret
		updateError  error
		wantGet      int
		wantUpdate   int
		wantConflict bool
	}{
		{name: "create"},
		{name: "no-op", existing: existingSecret("fresh", corev1.SecretTypeOpaque), wantGet: 1},
		{
			name: "update stale data and type",
			existing: func() *corev1.Secret {
				secret := existingSecret("stale", corev1.SecretTypeBasicAuth)
				secret.Data["spoke-only"] = []byte("remove")
				return secret
			}(),
			wantGet:    1,
			wantUpdate: 1,
		},
		{
			name:         "return update conflict for reconciliation retry",
			existing:     existingSecret("stale", corev1.SecretTypeOpaque),
			updateError:  errors.NewConflict(schema.GroupResource{Resource: "secrets"}, secretName, fmt.Errorf("conflict")),
			wantGet:      1,
			wantUpdate:   1,
			wantConflict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := fake.NewSimpleClientset(source.DeepCopy())
			spoke := fake.NewSimpleClientset()
			if tt.existing != nil {
				spoke = fake.NewSimpleClientset(tt.existing.DeepCopy())
			}
			if tt.updateError != nil {
				spoke.PrependReactor("update", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, tt.updateError
				})
			}

			r := &Reconciler{logger: zap.NewNop().Sugar(), hubKubeClient: hub}
			err := r.syncSecretToSpokeCluster(context.Background(), secretName, testClusterName, spoke, pipelineRun)
			if tt.wantConflict {
				assert.Assert(t, errors.IsConflict(err), err)
			} else {
				assert.NilError(t, err)
			}

			verbs := map[string]int{}
			for _, action := range spoke.Actions() {
				verbs[action.GetVerb()]++
				if action.GetVerb() == "update" {
					updated := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret)
					assert.Equal(t, updated.ResourceVersion, "7")
				}
			}
			assert.Equal(t, verbs["create"], 1)
			assert.Equal(t, verbs["get"], tt.wantGet)
			assert.Equal(t, verbs["update"], tt.wantUpdate)
			if tt.wantConflict {
				return
			}

			got, err := spoke.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
			assert.NilError(t, err)
			assert.DeepEqual(t, got.Data, source.Data)
			assert.Equal(t, got.Type, source.Type)
			if tt.existing == nil {
				assert.DeepEqual(t, got.Labels, source.Labels)
				assert.Equal(t, string(got.OwnerReferences[0].UID), string(pipelineRun.UID))
			} else {
				assert.DeepEqual(t, got.Labels, tt.existing.Labels)
				assert.DeepEqual(t, got.Annotations, tt.existing.Annotations)
				assert.DeepEqual(t, got.Finalizers, tt.existing.Finalizers)
				assert.DeepEqual(t, got.OwnerReferences, tt.existing.OwnerReferences)
			}
		})
	}
}
