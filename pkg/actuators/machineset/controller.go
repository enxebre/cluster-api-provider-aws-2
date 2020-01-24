package machineset

import (
	"context"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	providerconfigv1 "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsproviderconfig/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

type MachineSetReconciler struct {
	Client client.Client
	Log    logr.Logger

	recorder record.EventRecorder
	scheme   *runtime.Scheme
}

func (r *MachineSetReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&machinev1.MachineSet{}).
		WithOptions(options).
		Build(r)

	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	r.recorder = mgr.GetEventRecorderFor("machineset-controller")
	r.scheme = mgr.GetScheme()
	return nil
}

func (r *MachineSetReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("machineset", req.Name, "namespace", req.Namespace)
	logger.V(3).Info("Reconciling")

	ctx := context.Background()
	machineSet := &machinev1.MachineSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, machineSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return. Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Ignore deleted MachineSets, this can happen when foregroundDeletion
	// is enabled
	if !machineSet.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	originalMachineSetToPatch := client.MergeFrom(machineSet.DeepCopy())

	// TODO: Move this into its own reconcile logic
	providerConfig, err := getproviderConfig(*machineSet)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get providerConfig: %v", err)
	}
	instanceType := InstanceTypes[providerConfig.InstanceType]

	if machineSet.Annotations == nil {
		machineSet.Annotations = make(map[string]string)
	}

	// TODO: get annotations keys from machine API
	machineSet.Annotations["machine.openshift.io/vCPU"] = strconv.FormatInt(instanceType.VCPU, 10)
	machineSet.Annotations["machine.openshift.io/memoryMb"] = strconv.FormatInt(instanceType.VCPU, 10)
	machineSet.Annotations["machine.openshift.io/GPU"] = strconv.FormatInt(instanceType.VCPU, 10)

	if err := r.Client.Patch(ctx, machineSet, originalMachineSetToPatch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch machineSet: %v", err)
	}
	//result, err := r.reconcile(ctx, cluster, machineSet)
	//if err != nil {
	//	logger.Error(err, "Failed to reconcile MachineSet")
	//	r.recorder.Eventf(machineSet, corev1.EventTypeWarning, "ReconcileError", "%v", err)
	//}
	//return result, err
	return ctrl.Result{}, nil
}

func getproviderConfig(machineSet machinev1.MachineSet) (*providerconfigv1.AWSMachineProviderConfig, error) {
	codec, err := providerconfigv1.NewCodec()
	if err != nil {
		return nil, fmt.Errorf("failed to create codec: %v", err)
	}

	var providerConfig providerconfigv1.AWSMachineProviderConfig
	if err := codec.DecodeProviderSpec(&machineSet.Spec.Template.Spec.ProviderSpec, &providerConfig); err != nil {
		return nil, fmt.Errorf("failed to decode machineSet provider config: %v", err)
	}

	return &providerConfig, nil
}
