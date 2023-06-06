/*
Copyright 2023.

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

package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	pvv1alpha1 "github.com/GoogleContainerTools/kpt/porch/controllers/packagevariant/api/v1alpha1"
	pvsv1alpha1 "github.com/GoogleContainerTools/kpt/porch/controllers/packagevariantsets/api/v1alpha1"
	"github.com/go-logr/logr"
	deployv1alpha1 "github.com/nephio-project/api/nf_deployments/v1alpha1"
	reqv1alpha1 "github.com/nephio-project/api/nf_requirements/v1alpha1"
)

type DeployPolicyType string

const (
	DeployPolicyUPFBeforeSMF DeployPolicyType = "UPF_FIRST"
)

// NFTopologyReconciler reconciles a NFTopology object
type NFTopologyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	l      logr.Logger
}

func AllUPFsDeployed(nfTopo *reqv1alpha1.NFTopology, nfDeployed *deployv1alpha1.NFDeployed, nfInstanceName string) bool {
	return true
}

func BuildPVS(nfInst *reqv1alpha1.NFInstance, nfClassObj *reqv1alpha1.NFClass) *pvsv1alpha1.PackageVariantSet {
    retPVS := &pvsv1alpha1.PackageVariantSet{}

    retPVS.ObjectMeta.Name = nfInst.ObjectMeta.Name
    //retPVS.ObjectMeta.Namespace = nfInst.ObjectMeta.Namespace
    upstream := &pvv1alpha1.Upstream{}
    upstream.Repo = nfClassObj.PackageRef.RepositoryName
    upstream.Package = nfClassObj.PackageRef.PackageName
    upstream.Revision = nfClassObj.PackageRef.Revision

    retPVS.Spec.Upstream = upstream

    target := &pvsv1alpha1.Target{}
    objectSelector := &pvsv1alpha1.ObjectSelector{}

    labelMap := map[string]string{}
    for k, v := range nfInst.ClusterSelector.MatchLabels {
        labelMap[k] = v
    }

    objectSelector.LabelSelector = metav1.LabelSelector{MatchLabels: labelMap,}
    objectSelector.APIVersion = "infra.nephio.org/v1alpha1"
    objectSelector.Kind = "WorkloadCluster"

    retPVS.Spec.ObjectSelector = objectSelector

    // TODO(s3wong): adding Template in PVS?
    return retPVS
}

//+kubebuilder:rbac:groups=req.nephio.org,resources=nftopologies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=req.nephio.org,resources=nftopologies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=req.nephio.org,resources=nftopologies/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *NFTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.l = log.FromContext(ctx).WithValues("req", req)
	r.l.Info("Reconcile NFTopology")

	nfTopo := &reqv1alpha1.NFTopology{}
	if err := r.Get(ctx, req.NamespacedName, nfTopo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	nfDeployed := &deployv1alpha1.NFDeployed{}
	if err := r.Get(ctx, req.NamespacedName, nfDeployed); err != nil {
		r.l.Info(fmt.Sprintf("NF Deployed object for %s not created, continue...\n", nfTopo.ObjectMeta.Name))
	}

	// TODO(s3wong): turn the following into a configurable policy
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NFTopologyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// Uncomment the following line adding a pointer to an instance of the controlled resource as an argument
		For(&reqv1alpha1.NFTopology{}).
		Complete(r)
}
