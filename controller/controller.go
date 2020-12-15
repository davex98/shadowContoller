package controller

import (
	v12 "burghardt.tech/shadowController/pkg/apis/shadowresource/v1"
	clientset "burghardt.tech/shadowController/pkg/generated/clientset/versioned"
	samplescheme "burghardt.tech/shadowController/pkg/generated/clientset/versioned/scheme"
	informers "burghardt.tech/shadowController/pkg/generated/informers/externalversions/shadowresource/v1"
	listers "burghardt.tech/shadowController/pkg/generated/listers/shadowresource/v1"
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	v1inf "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"time"
)

const controllerAgentName = "shadow-controller"

const (
	SuccessSynced = "Synced"
	ErrResourceExists = "ErrResourceExists"
	MessageResourceExists = "Resource %q already exists and is not managed by Shadow"
	MessageResourceSynced = "Shadow synced successfully"
)


type Controller struct {
	kubeclientset kubernetes.Interface
	sampleclientset clientset.Interface

	podsSynced cache.InformerSynced
	shadowLister        listers.ShadowLister
	shadowSynced        cache.InformerSynced
	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder
	podsLister v1.PodLister
}

func NewController(
	kubeclientset kubernetes.Interface,
	sampleclientset clientset.Interface,
	podInformer v1inf.PodInformer,
	shadowInformer informers.ShadowInformer) *Controller {

	utilruntime.Must(samplescheme.AddToScheme(scheme.Scheme))
	klog.V(4).Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	controller := &Controller{
		kubeclientset:     kubeclientset,
		sampleclientset:   sampleclientset,
		podsLister: podInformer.Lister(),
		podsSynced: podInformer.Informer().HasSynced,
		shadowLister:        shadowInformer.Lister(),
		shadowSynced:        shadowInformer.Informer().HasSynced,
		workqueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Shadows"),
		recorder:          recorder,
	}

	klog.Info("Setting up event handlers")
	shadowInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueShadow,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueShadow(new)
		},
	})

	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.handleObject,
		UpdateFunc: func(old, new interface{}) {
			newPod := new.(*corev1.Pod)
			oldPod := old.(*corev1.Pod)
			if newPod.ResourceVersion == oldPod.ResourceVersion {
				return
			}
			controller.handleObject(new)
		},
		DeleteFunc: controller.handleObject,
	})

	return controller
}

func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("Starting Shadow controller")


	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.podsSynced, c.shadowSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}


func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	err := func(obj interface{}) error {
		defer c.workqueue.Done(obj)
		var key string
		var ok bool

		if key, ok = obj.(string); !ok {
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}

		if err := c.syncHandler(key); err != nil {
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}

		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

func (c *Controller) syncHandler(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	shadow, err := c.shadowLister.Shadows(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("shadow '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	podName := shadow.Spec.PodName
	if podName == "" {
		utilruntime.HandleError(fmt.Errorf("%s: pod name must be specified", key))
		return nil
	}

	pod, err := c.podsLister.Pods(shadow.Namespace).Get(podName)

	if errors.IsNotFound(err) {
		pod, err = c.kubeclientset.CoreV1().Pods(shadow.Namespace).Create(context.TODO(), NewPod(shadow), metav1.CreateOptions{})
	}

	if err != nil {
		return err
	}

	if !metav1.IsControlledBy(pod, shadow) {
		msg := fmt.Sprintf(MessageResourceExists, pod.Name)
		c.recorder.Event(shadow, corev1.EventTypeWarning, ErrResourceExists, msg)
		return fmt.Errorf(msg)
	}

	err = c.updateShadowStatus(shadow, pod)
	if err != nil {
		return err
	}

	c.recorder.Event(shadow, corev1.EventTypeNormal, SuccessSynced, "MessageResourceSynced")
	return nil
}

func (c *Controller) updateShadowStatus(shadow *v12.Shadow, pod *corev1.Pod) error {
	shCopy := shadow.DeepCopy()
	_, err := c.sampleclientset.BurghardtV1().Shadows(shadow.Namespace).Update(context.TODO(), shCopy, metav1.UpdateOptions{})
	return err
}


func (c *Controller) enqueueShadow(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		klog.V(4).Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	klog.V(4).Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {

		if ownerRef.Kind != "Shadow" {
			return
		}

		sh, err := c.shadowLister.Shadows(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			klog.V(4).Infof("ignoring orphaned object '%s' of shadow '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		c.enqueueShadow(sh)
		return
	}
}


func NewPod(shadow *v12.Shadow) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       shadow.Spec.PodName,
			Namespace:                  shadow.Namespace,
			OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(shadow, v12.SchemeGroupVersion.WithKind("Shadow")),
		},
		},
		Spec:       corev1.PodSpec{
			Containers: []corev1.Container{ {
					Name:  shadow.Spec.Image,
					Image: shadow.Spec.Image,
				}},
		},
	}
}