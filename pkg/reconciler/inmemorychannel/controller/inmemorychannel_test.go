/*
Copyright 2019 The Knative Authors

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
	"fmt"
	"testing"

	fakeeventingclient "knative.dev/eventing/pkg/client/injection/client/fake"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"

	"knative.dev/eventing/pkg/apis/eventing"

	"knative.dev/eventing/pkg/client/injection/reconciler/messaging/v1beta1/inmemorychannel"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	clientgotesting "k8s.io/client-go/testing"
	eventingduckv1beta1 "knative.dev/eventing/pkg/apis/duck/v1beta1"
	"knative.dev/eventing/pkg/apis/messaging/v1beta1"
	"knative.dev/eventing/pkg/reconciler/inmemorychannel/controller/resources"
	. "knative.dev/eventing/pkg/reconciler/testing"
	reconciletesting "knative.dev/eventing/pkg/reconciler/testing"
	"knative.dev/eventing/pkg/utils"
	"knative.dev/pkg/apis"
	duckv1beta1 "knative.dev/pkg/apis/duck/v1beta1"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/kmeta"
	logtesting "knative.dev/pkg/logging/testing"
	. "knative.dev/pkg/reconciler/testing"
)

const (
	systemNS              = "knative-testing"
	testNS                = "test-namespace"
	imcName               = "test-imc"
	channelServiceAddress = "test-imc-kn-channel.test-namespace.svc.cluster.local"
	imageName             = "test-image"

	imcGeneration = 7
)

func init() {
	// Add types to scheme
	_ = v1beta1.AddToScheme(scheme.Scheme)
	_ = duckv1beta1.AddToScheme(scheme.Scheme)
}

func TestAllCases(t *testing.T) {
	imcKey := testNS + "/" + imcName

	subscribers := []eventingduckv1beta1.SubscriberSpec{{
		UID:           "2f9b5e8e-deb6-11e8-9f32-f2801f1b9fd1",
		Generation:    1,
		SubscriberURI: apis.HTTP("call1"),
		ReplyURI:      apis.HTTP("sink2"),
	}, {
		UID:           "34c5aec8-deb6-11e8-9f32-f2801f1b9fd1",
		Generation:    2,
		SubscriberURI: apis.HTTP("call2"),
		ReplyURI:      apis.HTTP("sink2"),
	}}

	subscriberStatuses := []eventingduckv1beta1.SubscriberStatus{{
		UID:                "2f9b5e8e-deb6-11e8-9f32-f2801f1b9fd1",
		ObservedGeneration: 1,
		Ready:              "True",
	}, {
		UID:                "34c5aec8-deb6-11e8-9f32-f2801f1b9fd1",
		ObservedGeneration: 2,
		Ready:              "True",
	}}

	table := TableTest{
		{
			Name: "bad workqueue key",
			// Make sure Reconcile handles bad keys.
			Key: "too/many/parts",
		}, {
			Name: "key not found",
			// Make sure Reconcile handles good keys that don't exist.
			Key: "foo/not-found",
		}, {
			Name: "deleting",
			Key:  imcKey,
			Objects: []runtime.Object{
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeletedV1Beta1)},
			WantErr: false,
		}, {
			Name: "deployment does not exist",
			Key:  imcKey,
			Objects: []runtime.Object{
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentFailedV1Beta1("DispatcherDeploymentDoesNotExist", "Dispatcher Deployment does not exist")),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "DispatcherDeploymentFailed", `Reconciling dispatcher Deployment failed with: deployment.apps "imc-dispatcher" not found`),
			},
		}, {
			Name: "the status of deployment is false",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeFalseDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantErr: false,
			WantCreates: []runtime.Object{
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentFailedV1Beta1("DispatcherDeploymentFalse", "The status of Dispatcher Deployment is False: Deployment Failed : Deployment Failed"),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress)),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		}, {
			Name: "the status of deployment is unknown",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeUnknownDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantErr: false,
			WantCreates: []runtime.Object{
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentUnknownV1Beta1("DispatcherDeploymentUnknown", "The status of Dispatcher Deployment is Unknown: Deployment Unknown : Deployment Unknown"),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress)),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		}, {
			Name: "Service does not exist",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServicetNotReadyV1Beta1("DispatcherServiceDoesNotExist", "Dispatcher Service does not exist")),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "DispatcherServiceFailed", `Reconciling dispatcher Service failed: service "imc-dispatcher" not found`),
			},
		}, {
			Name: "Endpoints does not exist",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantErr: true,
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsNotReadyV1Beta1("DispatcherEndpointsDoesNotExist", "Dispatcher Endpoints does not exist"),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "InternalError", `endpoints "imc-dispatcher" not found`),
			},
		}, {
			Name: "Endpoints not ready",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeEmptyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantErr: true,
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsNotReadyV1Beta1("DispatcherEndpointsNotReady", "There are no endpoints ready for Dispatcher service"),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "InternalError", `there are no endpoints ready for Dispatcher service`),
			},
		}, {
			Name: "Works, creates new channel",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInMemoryChannelGenerationV1Beta1(imcGeneration)),
			},
			WantErr: false,
			WantCreates: []runtime.Object{
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelGenerationV1Beta1(imcGeneration),
					reconciletesting.WithInMemoryChannelStatusObservedGenerationV1Beta1(imcGeneration),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		}, {
			Name: "Works, channel exists",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantErr: false,
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		}, {
			Name: "channel exists, not owned by us",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
				makeChannelServiceNotOwnedByUs(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantErr: true,
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceNotReadyV1Beta1("ChannelServiceFailed", "Channel Service failed: inmemorychannel: test-namespace/test-imc does not own Service: \"test-imc-kn-channel\""),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "InternalError", `inmemorychannel: test-namespace/test-imc does not own Service: "test-imc-kn-channel"`),
			},
		}, {
			Name: "Works, channel exists with subscribers",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInMemoryChannelSubscribersV1Beta1(subscribers)),
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantErr: false,
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelSubscribersV1Beta1(subscribers),
					reconciletesting.WithInMemoryChannelStatusSubscribersV1Beta1(subscriberStatuses),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		}, {
			Name: "channel does not exist, fails to create",
			Key:  imcKey,
			Objects: []runtime.Object{
				makeReadyDeployment(),
				makeService(),
				makeReadyEndpoints(),
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS),
			},
			WantErr: true,
			WithReactors: []clientgotesting.ReactionFunc{
				InduceFailure("create", "Services"),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelDeploymentReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceNotReadyV1Beta1("ChannelServiceFailed", "Channel Service failed: inducing failure for create services"),
				),
			}},
			WantCreates: []runtime.Object{
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantEvents: []string{
				Eventf(corev1.EventTypeWarning, "InternalError", "inducing failure for create services"),
			},
		}, {},
	}

	logger := logtesting.TestLogger(t)
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			kubeClientSet:         fakekubeclient.Get(ctx),
			systemNamespace:       testNS,
			inmemorychannelLister: listers.GetV1Beta1InMemoryChannelLister(),
			// TODO: FIx
			inmemorychannelInformer: nil,
			deploymentLister:        listers.GetDeploymentLister(),
			serviceLister:           listers.GetServiceLister(),
			endpointsLister:         listers.GetEndpointsLister(),
		}
		return inmemorychannel.NewReconciler(ctx, logger,
			fakeeventingclient.Get(ctx), listers.GetV1Beta1InMemoryChannelLister(),
			controller.GetEventRecorder(ctx), r)
	},
		false,
		logger,
	))
}

func TestInNamespace(t *testing.T) {
	imcKey := testNS + "/" + imcName
	table := TableTest{
		{
			Name: "Works, creates new service account, role binding, dispatcher deployment and service and channel",
			Key:  imcKey,
			Objects: []runtime.Object{
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS, reconciletesting.WithInMemoryScopeAnnotationV1Beta1(eventing.ScopeNamespace)),
				makeRoleBinding(systemNS, dispatcherName+"-"+testNS, "eventing-config-reader", reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeReadyEndpoints(),
			},
			WantErr: false,
			WantCreates: []runtime.Object{
				makeServiceAccount(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeRoleBinding(testNS, dispatcherName, dispatcherName, reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeDispatcherDeployment(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeDispatcherService(testNS),
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInMemoryScopeAnnotationV1Beta1(eventing.ScopeNamespace),
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "DispatcherServiceAccountCreated", "Dispatcher ServiceAccount created"),
				Eventf(corev1.EventTypeNormal, "DispatcherRoleBindingCreated", "Dispatcher RoleBinding created"),
				Eventf(corev1.EventTypeNormal, "DispatcherDeploymentCreated", "Dispatcher Deployment created"),
				Eventf(corev1.EventTypeNormal, "DispatcherServiceCreated", "Dispatcher Service created"),
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		},
		{
			Name: "Works, existing service account, role binding, dispatcher deployment and service, new channel",
			Key:  imcKey,
			Objects: []runtime.Object{
				reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS, reconciletesting.WithInMemoryScopeAnnotationV1Beta1(eventing.ScopeNamespace)),
				makeServiceAccount(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeRoleBinding(testNS, dispatcherName, dispatcherName, reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeRoleBinding(systemNS, dispatcherName+"-"+testNS, "eventing-config-reader", reconciletesting.NewInMemoryChannelV1Beta1(imcName, "knative-testing")),
				makeDispatcherDeployment(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
				makeDispatcherService(testNS),
				makeReadyEndpoints(),
			},
			WantErr: false,
			WantCreates: []runtime.Object{
				makeChannelService(reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS)),
			},
			WantStatusUpdates: []clientgotesting.UpdateActionImpl{{
				Object: reconciletesting.NewInMemoryChannelV1Beta1(imcName, testNS,
					reconciletesting.WithInMemoryScopeAnnotationV1Beta1(eventing.ScopeNamespace),
					reconciletesting.WithInitInMemoryChannelConditionsV1Beta1,
					reconciletesting.WithInMemoryChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelEndpointsReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelChannelServiceReadyV1Beta1(),
					reconciletesting.WithInMemoryChannelAddressV1Beta1(channelServiceAddress),
				),
			}},
			WantEvents: []string{
				Eventf(corev1.EventTypeNormal, "InMemoryChannelReconciled", `InMemoryChannel reconciled: "test-namespace/test-imc"`),
			},
		},
	}

	logger := logtesting.TestLogger(t)
	table.Test(t, MakeFactory(func(ctx context.Context, listers *Listers, cmw configmap.Watcher) controller.Reconciler {
		r := &Reconciler{
			kubeClientSet:         fakekubeclient.Get(ctx),
			dispatcherImage:       imageName,
			systemNamespace:       systemNS,
			inmemorychannelLister: listers.GetV1Beta1InMemoryChannelLister(),
			// TODO: FIx
			inmemorychannelInformer: nil,
			deploymentLister:        listers.GetDeploymentLister(),
			serviceLister:           listers.GetServiceLister(),
			endpointsLister:         listers.GetEndpointsLister(),
			serviceAccountLister:    listers.GetServiceAccountLister(),
			roleBindingLister:       listers.GetRoleBindingLister(),
		}
		return inmemorychannel.NewReconciler(ctx, logger,
			fakeeventingclient.Get(ctx), listers.GetV1Beta1InMemoryChannelLister(),
			controller.GetEventRecorder(ctx), r)
	},
		false,
		logger,
	))
}

func makeDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      dispatcherName,
		},
		Status: appsv1.DeploymentStatus{},
	}
}

func makeReadyDeployment() *appsv1.Deployment {
	d := makeDeployment()
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
	return d
}

func makeFalseDeployment() *appsv1.Deployment {
	d := makeDeployment()
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionFalse, Reason: "Deployment Failed", Message: "Deployment Failed"}}
	return d
}

func makeUnknownDeployment() *appsv1.Deployment {
	d := makeDeployment()
	d.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionUnknown, Reason: "Deployment Unknown", Message: "Deployment Unknown"}}
	return d
}

func makeService() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      dispatcherName,
		},
	}
}

func makeServiceAccount(imc *v1beta1.InMemoryChannel) *corev1.ServiceAccount {
	return resources.MakeServiceAccount(imc.Namespace, dispatcherName)
}

func makeRoleBinding(ns, name, clusterRoleName string, imc *v1beta1.InMemoryChannel) *rbacv1.RoleBinding {
	return resources.MakeRoleBinding(ns, name, makeServiceAccount(imc), clusterRoleName)
}

func makeDispatcherDeployment(imc *v1beta1.InMemoryChannel) *appsv1.Deployment {
	return resources.MakeDispatcher(resources.DispatcherArgs{
		DispatcherName:      dispatcherName,
		DispatcherNamespace: testNS,
		Image:               imageName,
		ServiceAccountName:  dispatcherName,
	})
}

func makeDispatcherService(ns string) *corev1.Service {
	return resources.MakeDispatcherService(dispatcherName, ns)
}

func makeChannelService(imc *v1beta1.InMemoryChannel) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      fmt.Sprintf("%s-kn-channel", imcName),
			Labels: map[string]string{
				resources.MessagingRoleLabel: resources.MessagingRole,
			},
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(imc),
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: fmt.Sprintf("%s.%s.svc.%s", dispatcherName, testNS, utils.GetClusterDomainName()),
		},
	}
}

func makeChannelServiceNotOwnedByUs(imc *v1beta1.InMemoryChannel) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      fmt.Sprintf("%s-kn-channel", imcName),
			Labels: map[string]string{
				resources.MessagingRoleLabel: resources.MessagingRole,
			},
		},
		Spec: corev1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: fmt.Sprintf("%s.%s.svc.%s", dispatcherName, testNS, utils.GetClusterDomainName()),
		},
	}
}

func makeEmptyEndpoints() *corev1.Endpoints {
	return &corev1.Endpoints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Endpoints",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNS,
			Name:      dispatcherName,
		},
	}
}

func makeReadyEndpoints() *corev1.Endpoints {
	e := makeEmptyEndpoints()
	e.Subsets = []corev1.EndpointSubset{{Addresses: []corev1.EndpointAddress{{IP: "1.1.1.1"}}}}
	return e
}
