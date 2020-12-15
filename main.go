package main

import (
	controller "burghardt.tech/shadowController/controller"
	"burghardt.tech/shadowController/pkg/generated/clientset/versioned"
	informers "burghardt.tech/shadowController/pkg/generated/informers/externalversions"
	"flag"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

func main() {
	kubeconfig := getConfig()

	ch := SetupSignalHandler()
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	clientset, err := versioned.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	exampleInformerFactory := informers.NewSharedInformerFactory(clientset, time.Second*30)

	controller := controller.NewController(kubeClient, clientset,
		kubeInformerFactory.Core().V1().Pods(),
		exampleInformerFactory.Burghardt().V1().Shadows())


	kubeInformerFactory.Start(ch)
	exampleInformerFactory.Start(ch)

	if err = controller.Run(2, ch); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}


func getConfig() *string {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	return kubeconfig
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE")
}


func SetupSignalHandler() (stopCh <-chan struct{}) {

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1)
	}()

	return stop
}