/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// CABundleReconciler reconciles a ConfigMap object
type CABundleReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	TargetNamespace string
	EventCh         chan event.GenericEvent
}

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMap object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/reconcile
func (r *CABundleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	Logger := logf.FromContext(ctx)
	Logger.Info("Reconciling CA bundles", "namespace", req.Namespace, "name", req.Name)

	// Read teh ConfigMap to get the URL list
	var cm corev1.ConfigMap
	err := r.Get(ctx, req.NamespacedName, &cm)
	if err != nil {
		Logger.Error(err, "unable to fetch ConfigMap")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	baseUrl, ok := cm.Data["bundle_url"]
	if !ok {
		Logger.Error(err, "bundle_url key not found in ConfigMap data")
		return ctrl.Result{}, nil
	}

	httpCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	bundles, err := DownloadPEMBundles(httpCtx, baseUrl)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, b := range bundles {
		// Check if ConfigMap already exists for this bundle and mathches content
		exists := r.checkConfigMap(ctx, b)
		if exists {
			continue
		}
		// if the ConfigMap does not exist or content differs, create or update it
		err := r.createOrUpdateConfigMap(ctx, b)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	// Finally Clean up stale ConfigMaps
	err = r.CleanUpConfigMaps(ctx, bundles)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CABundleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	src := source.TypedChannel(
		r.EventCh,
		&handler.EnqueueRequestForObject{},
	)

	return ctrl.NewControllerManagedBy(mgr).
		WatchesRawSource(src).
		Named("cabundle-operator").
		Complete(r)
}
