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
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//porchv1alpha1 "github.com/GoogleContainerTools/kpt/porch/api/porch/v1alpha1"
	nfdeployv1alpha1 "github.com/s3wong/api/nf_deployments/v1alpha1"
	nfreqv1alpha1 "github.com/s3wong/api/nf_requirements/v1alpha1"
	porchv1alpha1 "github.com/s3wong/porchapi/api/v1alpha1"
)

type PackageRevisionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	delimiter = "--"
)

func ConstructNFDeployedName(nfInstanceName string, regionName string, clusterName string) string {
	return nfInstanceName + delimiter + regionName + delimiter + clusterName
}

func GetNFInstanceNameFromNFDeployedName(nfDeployedName string) string {
	return strings.Split(nfDeployedName, delimiter)[0]
}

func BuildAttachmentMap(nfTopo *nfreqv1alpha1.NFTopology) map[string][]string {
	ret := make(map[string][]string)
	nfTopoSpec := nfTopo.Spec
	for _, nfInst := range nfTopoSpec.NFInstances {
		fmt.Printf("SKW: NF Topology's NF Instance is %v\n", nfInst)
		for _, nfAttachment := range nfInst.NFTemplate.NFAttachments {
			fmt.Printf("SKW: %s is connected to %s\n", nfAttachment.NetworkInstanceName, nfInst.Name)
			ret[nfAttachment.NetworkInstanceName] = append(ret[nfAttachment.NetworkInstanceName], nfInst.Name)
		}
	}
	fmt.Printf("SKW: the attachment map is %v\n", ret)
	return ret
}

func BuildNFDeployed(nfDeployedName string, topoNamespace string, nfTopo *nfreqv1alpha1.NFTopology, nfInstance *nfreqv1alpha1.NFInstance, clusterName string, vendor string, version string, nfdeployed *nfdeployv1alpha1.NFDeployed) error {
	var nfdeployedInstance *nfdeployv1alpha1.NFDeployedInstance = nil

	for _, nfInst := range nfdeployed.Spec.NFInstances {
		if nfInst.Id == nfDeployedName {
			nfdeployedInstance = &nfInst
			break
		}
	}

	if nfdeployedInstance == nil {
		nfdeployedInstance = &nfdeployv1alpha1.NFDeployedInstance{}
	}
	nfTemplate := nfInstance.NFTemplate
	nfdeployedInstance.Id = nfDeployedName
	nfdeployedInstance.ClusterName = clusterName
	nfdeployedInstance.NFType = string(nfTemplate.NFType)
	nfdeployedInstance.NFVendor = vendor
	nfdeployedInstance.NFVersion = version

	attachmentMap := BuildAttachmentMap(nfTopo)

	for _, nfAttachment := range nfTemplate.NFAttachments {
		if instList, ok := attachmentMap[nfAttachment.NetworkInstanceName]; ok {
			for _, instName := range instList {
				if instName != nfInstance.Name {
					con := nfdeployv1alpha1.NFDeployedConnectivity{}
					con.NeighborName = instName
					nfdeployedInstance.Connectivities = append(nfdeployedInstance.Connectivities, con)
				}
			}
		}
	}
	nfdeployed.Spec.NFInstances = append(nfdeployed.Spec.NFInstances, *nfdeployedInstance)
	return nil
}

// +kubebuilder:rbac:groups=porch.kpt.dev,resources=packagerevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=porch.kpt.dev,resources=packagerevisions/status,verbs=get;update;patch
// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PackageRevisionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("NFTopology-PackageRevision", req.NamespacedName)

	pkgRev := &porchv1alpha1.PackageRevision{}
	if err := r.Get(ctx, req.NamespacedName, pkgRev); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Step 1: check package revision state
	// TODO(s3wong): if package deleted, inform ESA
	if pkgRev.Spec.Lifecycle != porchv1alpha1.PackageRevisionLifecyclePublished {
		log.Info("Package lifecycle is not Published, ignore", "PackageRevision.lifecycle",
			pkgRev.Spec.Lifecycle)
		return ctrl.Result{}, nil
	}

	var regionName, clusterName, nfInstanceName, topologyName, topoNamespace string
	var createDeployed, found bool
	pkgRevLabels := pkgRev.ObjectMeta.Labels
	if regionName, found = pkgRevLabels["region"]; !found {
		log.Info("region name not found from package")
		return ctrl.Result{}, nil
	}
	if nfInstanceName, found = pkgRevLabels["nfinstance"]; !found {
		log.Info("NF Instance name not found from package")
		return ctrl.Result{}, nil
	}
	if topologyName, found = pkgRevLabels["nftopology"]; !found {
		log.Info("NF Topology name not found from package")
		return ctrl.Result{}, nil
	}
	if topoNamespace, found = pkgRevLabels["nftopology-namespace"]; !found {
		log.Info("NF Topology namespace not found from package")
		return ctrl.Result{}, nil
	}
	// repo name is the cluster name
	clusterName = pkgRev.Spec.RepositoryName

	// Step 2: search for NFTopology and NFDeployed
	nfTopology := &nfreqv1alpha1.NFTopology{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: topoNamespace, Name: topologyName}, nfTopology); err != nil {
		log.Error(err, fmt.Sprintf("NFTopology object not found: %s\n", topologyName))
		return ctrl.Result{}, nil
	}

	// TODO(s3wong): potentially, there would be multiple NFDeployedInstance CRs for an instance of
	// NFInstance --- which means deploymentName should be unique
	deploymentName := ConstructNFDeployedName(nfInstanceName, regionName, clusterName)

	nfDeployed := &nfdeployv1alpha1.NFDeployed{}
	if err := r.Client.Get(ctx, client.ObjectKey{Namespace: topoNamespace, Name: nfTopology.ObjectMeta.Name}, nfDeployed); err != nil {
		createDeployed = true
		nfDeployed.ObjectMeta.Name = nfTopology.ObjectMeta.Name
		nfDeployed.ObjectMeta.Namespace = nfTopology.ObjectMeta.Namespace
	}

	// Search for corresponding NFInstance
	var nfInstance *nfreqv1alpha1.NFInstance = nil
	for _, nfInst := range nfTopology.Spec.NFInstances {
		if nfInst.Name == nfInstanceName {
			nfInstance = &nfInst
			break
		}
	}
	if nfInstance == nil {
		err := errors.New(fmt.Sprintf("%s not found in NF Topology %s\n", nfInstanceName, topologyName))
		log.Error(err, fmt.Sprintf("NF Instance %s not found in NF Topology %s\n", nfInstanceName, topologyName))
		return ctrl.Result{}, nil
	}
	// Get NFClass, cluster scope
	className := nfInstance.NFTemplate.ClassName
	var nfClass = &nfreqv1alpha1.NFClass{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: className}, nfClass); err != nil {
		log.Error(err, fmt.Sprintf("NFClass object not found: %s: %s\n", className, err.Error()))
		return ctrl.Result{}, nil
	}
	if err := BuildNFDeployed(deploymentName, topoNamespace, nfTopology, nfInstance, clusterName, nfClass.Spec.Vendor, nfClass.Spec.Version, nfDeployed); err != nil {
		log.Error(err, fmt.Sprintf("Failed to build NFDeployed %s: %s\n", deploymentName, err.Error()))
		return ctrl.Result{}, nil
	}

	if createDeployed {
		if err := r.Client.Create(ctx, nfDeployed); err != nil {
			log.Error(err, fmt.Sprintf("Failed to create NFDeployed %s: %s\n", deploymentName, err.Error()))
			return ctrl.Result{}, nil
		}
	} else {
		if err := r.Client.Update(ctx, nfDeployed); err != nil {
			log.Error(err, fmt.Sprintf("Failed to update NFDeployed %s: %s\n", deploymentName, err.Error()))
			return ctrl.Result{}, nil
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PackageRevisionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&porchv1alpha1.PackageRevision{}).
		Complete(r)
}
