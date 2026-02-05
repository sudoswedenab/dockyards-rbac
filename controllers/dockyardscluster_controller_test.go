package controllers

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	dyconfig "github.com/sudoswedenab/dockyards-backend/api/config"
	dockyardsv1 "github.com/sudoswedenab/dockyards-backend/api/v1alpha3"
	"github.com/sudoswedenab/dockyards-rbac/test/mockcrds"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestClusterReconciler(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("no kubebuilder assets configured")
	}

	env := envtest.Environment{
		CRDs: mockcrds.CRDs,
	}

	textHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError})
	slogr := logr.FromSlogHandler(textHandler)

	ctrl.SetLogger(slogr)

	ctx, cancel := context.WithCancel(context.Background())

	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		cancel()
		err := env.Stop()
		if err != nil {
			panic(err)
		}
	})

	scheme := runtime.NewScheme()

	_ = corev1.AddToScheme(scheme)
	_ = dockyardsv1.AddToScheme(scheme)

	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	mgr, err := manager.New(cfg, manager.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		err := mgr.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatalf("could not sync cache")
	}

	dockyardsConfigManager := dyconfig.NewFakeConfigManager(map[string]string{"publicNamespace": "dockyards-public"})

	t.Run("test cluster reconciliation", func(t *testing.T) {
		namespace := corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-ns-",
			},
		}

		err = c.Create(ctx, &namespace)
		if err != nil {
			t.Fatal(err)
		}

		organization := dockyardsv1.Organization{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-org-",
				Namespace:    namespace.Name,
			},
		}

		err = c.Create(ctx, &organization)
		if err != nil {
			t.Fatal(err)
		}

		cluster := dockyardsv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "test-cluster-",
				Namespace:    organization.Namespace,
			},
		}

		err = c.Create(ctx, &cluster)
		if err != nil {
			t.Fatal(err)
		}

		r := DockyardsClusterReconciler{c, dockyardsConfigManager}
		_, err = r.reconcileRBACWorkload(ctx, &cluster)
		if err != nil {
			t.Fatal(err)
		}

		workload := dockyardsv1.Workload{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + "-rbac",
				Namespace: cluster.Namespace,
			},
		}
		err = c.Get(ctx, client.ObjectKeyFromObject(&workload), &workload)
		if err != nil {
			t.Fatal(err)
		}

		raw := *workload.Spec.Input
		parsed := RBACWorkloadInput{}
		err = json.Unmarshal(raw.Raw, &parsed)
		if err != nil {
			t.Fatal(err)
		}

		readerRole := RolePrefix + strings.ToLower(dockyardsv1.RoleReader)
		editorRole := RolePrefix + strings.ToLower(dockyardsv1.RoleUser)
		adminRole := RolePrefix + strings.ToLower(dockyardsv1.RoleSuperUser)

		expected := RBACWorkloadInput{
			ClusterRoleBindings: []RoleBinding{
				{
					BindingName: readerRole,
					RoleRef: rbacv1.RoleRef{
						Kind: "ClusterRole",
						Name: "view",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind: "Group",
							Name: readerRole,
						},
					},
				},
				{
					BindingName: editorRole,
					RoleRef: rbacv1.RoleRef{
						Kind: "ClusterRole",
						Name: "edit",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind: "Group",
							Name: editorRole,
						},
					},
				},
				{
					BindingName: adminRole,
					RoleRef: rbacv1.RoleRef{
						Kind: "ClusterRole",
						Name: "admin",
					},
					Subjects: []rbacv1.Subject{
						{
							Kind: "Group",
							Name: adminRole,
						},
					},
				},
			},
		}

		if !cmp.Equal(expected, parsed) {
			t.Errorf("diff: %s", cmp.Diff(expected, parsed))
		}
	})
}
