// Copyright 2025 Sudo Sweden AB
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sudoswedenab/dockyards-backend/api/apiutil"
	dyconfig "github.com/sudoswedenab/dockyards-backend/api/config"
	dockyardsv1 "github.com/sudoswedenab/dockyards-backend/api/v1alpha3"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type DockyardsClusterReconciler struct {
	client.Client
	*dyconfig.ConfigManager
}

// +kubebuilder:rbac:groups=dockyards.io,resources=clusters;organizations;memebers,verbs=get;list;watch
// +kubebuilder:rbac:groups=dockyards.io,resources=clusters/status,verbs=patch
// +kubebuilder:rbac:groups=dockyards.io,resources=workloads,verbs=create;patch;get;list;watch

func (r *DockyardsClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	var cluster dockyardsv1.Cluster
	err := r.Get(ctx, req.NamespacedName, &cluster)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if !cluster.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	ownerOrganization, err := apiutil.GetOwnerOrganization(ctx, r.Client, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ownerOrganization == nil {
		logger.Info("ignoring cluster without owner organization")

		return ctrl.Result{}, nil
	}

	return r.reconcileRBACWorkload(ctx, &cluster)
}

func (r *DockyardsClusterReconciler) reconcileRBACWorkload(ctx context.Context, cluster *dockyardsv1.Cluster) (ctrl.Result, error) {
	logger := ctrl.LoggerFrom(ctx)

	publicNamespace := r.GetValueOrDefault(dyconfig.KeyPublicNamespace, "")
	if publicNamespace == "" {
		return ctrl.Result{}, fmt.Errorf("no value for config key `%s`", dyconfig.KeyPublicNamespace)
	}

	workload := dockyardsv1.Workload{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-rbac",
			Namespace: cluster.Namespace,
		},
	}

	operationResult, err := controllerutil.CreateOrPatch(ctx, r.Client, &workload, func() error {
		if metav1.HasAnnotation(workload.ObjectMeta, dockyardsv1.AnnotationSkipRemediation) {
			return nil
		}

		workload.Labels = map[string]string{
			dockyardsv1.LabelClusterName: cluster.Name,
		}

		workload.OwnerReferences = []metav1.OwnerReference{
			{
				APIVersion: dockyardsv1.GroupVersion.String(),
				Kind:       dockyardsv1.ClusterKind,
				Name:       cluster.Name,
				UID:        cluster.UID,
			},
		}

		workload.Spec.Provenience = dockyardsv1.ProvenienceDockyards
		workload.Spec.ClusterComponent = true
		workload.Spec.TargetNamespace = "kube-system"

		workload.Spec.WorkloadTemplateRef = &corev1.TypedObjectReference{
			Kind:      dockyardsv1.WorkloadTemplateKind,
			Name:      "rbac",
			Namespace: &publicNamespace,
		}

		readerRole := "dockyards:" + strings.ToLower(dockyardsv1.RoleReader)
		editorRole := "dockyards:" + strings.ToLower(dockyardsv1.RoleUser)
		adminRole := "dockyards:" + strings.ToLower(dockyardsv1.RoleSuperUser)

		raw, err := json.Marshal(map[string]any{
			"clusterRoleBindings": []map[string]any{
				{
					"bindingName": readerRole,
					"roleRef": map[string]string{
						"kind": "ClusterRole",
						"name": "view",
					},
					"subjects": []map[string]string{
						{
							"kind": "Group",
							"name": readerRole,
						},
					},
				},
				{
					"bindingName": editorRole,
					"roleRef": map[string]string{
						"kind": "ClusterRole",
						"name": "edit",
					},
					"subjects": []map[string]string{
						{
							"kind": "Group",
							"name": editorRole,
						},
					},
				},
				{
					"bindingName": adminRole,
					"roleRef": map[string]string{
						"kind": "ClusterRole",
						"name": "cluster-admin",
					},
					"subjects": []map[string]string{
						{
							"kind": "Group",
							"name": adminRole,
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}

		workload.Spec.Input = &apiextensionsv1.JSON{
			Raw: raw,
		}

		return nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciled Workload", "cluster", cluster.Name, "workload", workload.Name, "operationResult", operationResult)

	return ctrl.Result{}, nil
}

// SetupWithManager configures the controller runtime to manage cluster resources
func (r *DockyardsClusterReconciler) SetupWithManager(manager ctrl.Manager) error {
	scheme := manager.GetScheme()

	_ = dockyardsv1.AddToScheme(scheme)

	err := ctrl.NewControllerManagedBy(manager).
		For(&dockyardsv1.Cluster{}).
		Complete(r)
	if err != nil {
		return err
	}

	return nil
}
