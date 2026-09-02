package config

import (
	"bytes"
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

func TestHubCoreRBAC(t *testing.T) {
	data, err := os.ReadFile("rbac.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var role rbacv1.ClusterRole
	for _, document := range bytes.Split(data, []byte("\n---\n")) {
		var candidate rbacv1.ClusterRole
		if err := yaml.Unmarshal(document, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.Kind == "ClusterRole" {
			role = candidate
			break
		}
	}
	if role.Kind == "" {
		t.Fatal("ClusterRole not found")
	}

	want := map[string][]string{
		"configmaps": {"get", "list", "watch"},
		"secrets":    {"get"},
	}
	seen := make(map[string]bool, len(want))
	for _, rule := range role.Rules {
		if !slices.Equal(rule.APIGroups, []string{""}) {
			continue
		}
		for _, resource := range rule.Resources {
			if verbs, ok := want[resource]; ok {
				seen[resource] = true
				got := slices.Clone(rule.Verbs)
				slices.Sort(got)
				if !slices.Equal(got, verbs) {
					t.Errorf("%s verbs = %v, want %v", resource, rule.Verbs, verbs)
				}
			}
			if resource == "persistentvolumeclaims" || resource == "serviceaccounts" {
				t.Errorf("unused resource %s must not be granted", resource)
			}
		}
	}
	for resource := range want {
		if !seen[resource] {
			t.Errorf("missing %s rule", resource)
		}
	}
}
