package controllers

import rbacv1 "k8s.io/api/rbac/v1"

type RoleBinding struct {
	BindingName string           `json:"bindingName"`
	RoleRef     rbacv1.RoleRef   `json:"roleRef"`
	Subjects    []rbacv1.Subject `json:"subjects"`
}

type RBACWorkloadInput struct {
	ClusterRoleBindings []RoleBinding `json:"clusterRoleBindings"`
	RoleBindings        []RoleBinding `json:"roleBindings"`
}
