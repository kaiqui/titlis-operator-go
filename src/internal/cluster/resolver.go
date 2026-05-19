package cluster

import (
	"context"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	identityConfigMapName = "titlis-cluster-identity"
	identityKey           = "cluster-name"
)

// nodeClusterLabels lists cloud-provider labels that carry the cluster name, in priority order.
var nodeClusterLabels = []string{
	"eks.amazonaws.com/cluster-name",
	"alpha.eksctl.io/cluster-name",
	"cloud.google.com/gke-cluster-name",
	"kubernetes.azure.com/cluster",
}

// ResolveClusterName returns the cluster name and the source that provided it.
//
// Priority:
//  1. KUBERNETES_CLUSTER_NAME env var — always wins, never persisted
//  2. ConfigMap titlis-cluster-identity/<identityKey> — sealed on first run
//  3. Cloud-provider Node labels (EKS, GKE, AKS)
//
// When the name is first resolved via Node labels it is written to the ConfigMap
// so that a future label change (e.g. cloud-provider rename) does not silently
// create a second cluster in the database.
func ResolveClusterName(ctx context.Context, c client.Client, namespace string) (name, source string) {
	if v := os.Getenv("KUBERNETES_CLUSTER_NAME"); v != "" {
		return v, "env"
	}

	var cm corev1.ConfigMap
	cmKey := types.NamespacedName{Namespace: namespace, Name: identityConfigMapName}
	if err := c.Get(ctx, cmKey, &cm); err == nil {
		if v := cm.Data[identityKey]; v != "" {
			return v, "configmap"
		}
	}

	if v := resolveFromNodeLabels(ctx, c); v != "" {
		sealIdentity(ctx, c, namespace, v)
		return v, "node-labels"
	}

	return "unknown", "fallback"
}

func resolveFromNodeLabels(ctx context.Context, c client.Client) string {
	var nodeList corev1.NodeList
	if err := c.List(ctx, &nodeList, &client.ListOptions{Limit: 1}); err != nil || len(nodeList.Items) == 0 {
		return ""
	}
	labels := nodeList.Items[0].Labels
	for _, key := range nodeClusterLabels {
		if v := labels[key]; v != "" {
			return v
		}
	}
	return ""
}

func sealIdentity(ctx context.Context, c client.Client, namespace, name string) {
	var cm corev1.ConfigMap
	cmKey := types.NamespacedName{Namespace: namespace, Name: identityConfigMapName}

	if err := c.Get(ctx, cmKey, &cm); err != nil {
		newCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      identityConfigMapName,
				Namespace: namespace,
			},
			Data: map[string]string{identityKey: name},
		}
		_ = c.Create(ctx, newCM)
		return
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[identityKey] = name
	_ = c.Update(ctx, &cm)
}
