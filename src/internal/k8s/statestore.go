package k8s

import (
	"context"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StateStore persists small key/value blobs in a Kubernetes ConfigMap.
// Used by controllers to survive restarts without losing ephemeral state.
type StateStore struct {
	k8s       client.Client
	namespace string
	name      string
}

func NewStateStore(k8s client.Client, namespace, name string) *StateStore {
	return &StateStore{k8s: k8s, namespace: namespace, name: name}
}

// Get decodes the JSON value stored under key into dest.
// Returns (false, nil) when the key does not exist.
func (s *StateStore) Get(ctx context.Context, key string, dest any) (bool, error) {
	cm, err := s.get(ctx)
	if err != nil || cm == nil {
		return false, err
	}
	raw, ok := cm.Data[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		return false, err
	}
	return true, nil
}

// Set JSON-encodes value and stores it under key, creating the ConfigMap if needed.
func (s *StateStore) Set(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	cm, err := s.get(ctx)
	if err != nil {
		return err
	}

	if cm == nil {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      s.name,
				Namespace: s.namespace,
				Labels:    map[string]string{"app.kubernetes.io/managed-by": "titlis-operator"},
			},
			Data: map[string]string{key: string(data)},
		}
		return s.k8s.Create(ctx, cm)
	}

	patch := client.MergeFrom(cm.DeepCopy())
	if cm.Data == nil {
		cm.Data = map[string]string{}
	}
	cm.Data[key] = string(data)
	return s.k8s.Patch(ctx, cm, patch)
}

// Delete removes a key. No-ops if key or ConfigMap doesn't exist.
func (s *StateStore) Delete(ctx context.Context, key string) error {
	cm, err := s.get(ctx)
	if err != nil || cm == nil {
		return err
	}
	if _, ok := cm.Data[key]; !ok {
		return nil
	}
	patch := client.MergeFrom(cm.DeepCopy())
	delete(cm.Data, key)
	return s.k8s.Patch(ctx, cm, patch)
}

func (s *StateStore) get(ctx context.Context) (*corev1.ConfigMap, error) {
	var cm corev1.ConfigMap
	err := s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: s.name}, &cm)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &cm, nil
}
